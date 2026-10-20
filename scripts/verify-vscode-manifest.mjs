#!/usr/bin/env node
// 校验 VSCode 扩展的清单与语言配置。
//
// 为什么需要这个脚本：
//   这两类错误 VSCode **不会报错**，只会静默降级，很难被发现：
//     1. language-configuration 不是合法 JSON → 整份配置被丢弃，
//        行注释/括号/自动闭合全部失效，但语言识别照常работа；
//     2. indentationRules 的正则用了 JS 不支持的语法（如 Oniguruma 的 `(?i)`）
//        → console.warn 后该条规则被丢弃，缩进静默不生效。
//
// 结论：能用自动校验兜住的，绝不靠肉眼看。
//
// 用法：
//   node scripts/verify-vscode-manifest.mjs
import * as fs from "node:fs";
import * as path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, "..", "..");
const extDir = path.join(root, "vscode-extension");

let failures = 0;
let checks = 0;

function fail(msg) {
  failures++;
  console.error(`  ✗ ${msg}`);
}

function ok(msg) {
  checks++;
  console.log(`  ✓ ${msg}`);
}

/** 读 JSON；失败时抛出带文件名的错误。 */
function readJSON(rel) {
  const file = path.join(extDir, rel);
  const raw = fs.readFileSync(file, "utf8");
  try {
    return JSON.parse(raw);
  } catch (err) {
    throw new Error(`${rel} 不是合法 JSON：${err.message}`);
  }
}

// ---------------------------------------------------------------------------
// 1. 清单结构
// ---------------------------------------------------------------------------

function verifyManifest() {
  console.log("清单 package.json");
  let manifest;
  try {
    manifest = readJSON("package.json");
    ok("是合法 JSON");
  } catch (err) {
    fail(err.message);
    return null;
  }

  const contributes = manifest.contributes ?? {};
  const languages = contributes.languages ?? [];

  // 每个语言 id 必须唯一，且扩展名不与别的语言冲突
  const seenIds = new Map();
  const seenExts = new Map();
  for (const lang of languages) {
    const id = lang.id;
    if (!id) {
      fail("有语言条目缺少 id");
      continue;
    }
    if (seenIds.has(id)) {
      fail(`语言 id 重复：${id}`);
    }
    seenIds.set(id, lang);

    for (const ext of lang.extensions ?? []) {
      const key = ext.toLowerCase();
      if (seenExts.has(key)) {
        fail(`扩展名 ${ext} 被 ${seenExts.get(key)} 与 ${id} 同时声明`);
      }
      seenExts.set(key, id);
    }
  }

  // 本项目要求：.4gl/.global → bdl，.per → per
  const expected = [
    [".4gl", "bdl"],
    [".global", "bdl"],
    [".per", "per"],
  ];
  for (const [ext, wantID] of expected) {
    const got = seenExts.get(ext);
    if (got === wantID) {
      ok(`${ext} → ${wantID}`);
    } else if (got === undefined) {
      fail(`${ext} 没有任何语言声明（期望 ${wantID}）`);
    } else {
      fail(`${ext} 声明给了 ${got}，期望 ${wantID}`);
    }
  }

  // 语言配置路径必须存在
  for (const lang of languages) {
    const cfg = lang.configuration;
    if (!cfg) {
      fail(`语言 ${lang.id} 没有 configuration`);
      continue;
    }
    const rel = cfg.replace(/^\.\//, "");
    if (fs.existsSync(path.join(extDir, rel))) {
      ok(`${lang.id} 的 configuration 存在：${rel}`);
    } else {
      fail(`语言 ${lang.id} 的 configuration 指向不存在的文件：${cfg}`);
    }
  }

  // 语法文件必须存在，且 scopeName 与 language 对应
  for (const g of contributes.grammars ?? []) {
    const rel = (g.path ?? "").replace(/^\.\//, "");
    if (!fs.existsSync(path.join(extDir, rel))) {
      fail(`语法文件不存在：${g.path}`);
      continue;
    }
    try {
      const grammar = JSON.parse(fs.readFileSync(path.join(extDir, rel), "utf8"));
      if (grammar.scopeName !== g.scopeName) {
        fail(`${rel} 的 scopeName=${grammar.scopeName} 与清单声明的 ${g.scopeName} 不一致`);
      } else {
        ok(`${g.language} 语法 ${rel}（${g.scopeName}）`);
      }
    } catch (err) {
      fail(`${rel} 不是合法 JSON：${err.message}`);
    }
  }

  // 激活事件里的每次 onLanguage 都要有对应语言
  for (const ev of manifest.activationEvents ?? []) {
    const m = /^onLanguage:(.+)$/.exec(ev);
    if (m && !seenIds.has(m[1])) {
      fail(`激活事件 ${ev} 引用了未声明的语言 ${m[1]}`);
    }
  }

  return manifest;
}

// ---------------------------------------------------------------------------
// 2. 语言配置
// ---------------------------------------------------------------------------

/**
 * 校验一个正则字段。
 *
 * VSCode 允许两种写法：字符串（无 flags）或 { pattern, flags }。
 * 字符串里写 `(?i)` 是 Oniguruma 语法，JS 会直接抛错，
 * VSCode 只 warn 然后丢弃该规则——这正是之前踩的坑。
 */
function checkRegexField(label, value) {
  let pattern;
  let flags = "";

  if (typeof value === "string") {
    pattern = value;
  } else if (value && typeof value === "object") {
    pattern = value.pattern;
    flags = value.flags ?? "";
    if (typeof pattern !== "string") {
      fail(`${label}: pattern 必须是字符串`);
      return;
    }
    if (!/^[gimuy]*$/.test(flags)) {
      fail(`${label}: flags 非法（${flags}），VSCode 只接受 g/i/m/u/y`);
      return;
    }
  } else {
    fail(`${label}: 必须是字符串或 {pattern, flags} 对象`);
    return;
  }

  // 常见错误：把 Oniguruma 行内标志写进 JS 正则
  if (pattern.includes("(?i)") || pattern.includes("(?-i)")) {
    fail(`${label}: 使用了 JS 不支持的 (?i)，请改用 flags: "i"`);
    return;
  }

  try {
    new RegExp(pattern, flags);
    ok(`${label} 正则合法${flags ? `（flags=${flags}）` : ""}`);
  } catch (err) {
    fail(`${label} 正则非法：${err.message}`);
  }
}

/** 校验一对括号结构的完整性（open/close 成对出现）。 */
function checkPairList(label, value, isObject) {
  if (!Array.isArray(value)) {
    fail(`${label}: 必须是数组`);
    return;
  }
  for (const item of value) {
    if (isObject) {
      if (typeof item?.open !== "string" || typeof item?.close !== "string") {
        fail(`${label}: 条目缺少 open/close：${JSON.stringify(item)}`);
        return;
      }
    } else if (!Array.isArray(item) || item.length !== 2) {
      fail(`${label}: 条目必须是 [open, close]，实际 ${JSON.stringify(item)}`);
      return;
    }
  }
  ok(`${label} 结构正确（${value.length} 项）`);
}

function verifyLanguageConfig(rel) {
  console.log(`\n语言配置 ${rel}`);
  let cfg;
  try {
    cfg = readJSON(rel);
    ok("是合法 JSON");
  } catch (err) {
    fail(err.message);
    return;
  }

  if (cfg.comments) {
    if (cfg.comments.lineComment !== undefined && typeof cfg.comments.lineComment !== "string") {
      fail("comments.lineComment 必须是字符串");
    }
    const bc = cfg.comments.blockComment;
    if (bc !== undefined) {
      if (!Array.isArray(bc) || bc.length !== 2) {
        fail("comments.blockComment 必须是 [开始, 结束]");
      }
    }
    ok(`行注释 = ${cfg.comments.lineComment ?? "（无）"}`);
  }

  if (cfg.brackets) {
    checkPairList("brackets", cfg.brackets, false);
  }
  if (cfg.autoClosingPairs) {
    checkPairList("autoClosingPairs", cfg.autoClosingPairs, true);
  }
  if (cfg.surroundingPairs) {
    checkPairList("surroundingPairs", cfg.surroundingPairs, false);
  }

  if (cfg.wordPattern !== undefined) {
    checkRegexField("wordPattern", cfg.wordPattern);
  }

  if (cfg.indentationRules) {
    for (const [key, value] of Object.entries(cfg.indentationRules)) {
      checkRegexField(`indentationRules.${key}`, value);
    }
  }
}

// ---------------------------------------------------------------------------

const manifest = verifyManifest();
if (manifest) {
  for (const rel of [
    "language-configuration/bdl.json",
    "language-configuration/per.json",
  ]) {
    verifyLanguageConfig(rel);
  }
}

console.log("");
if (failures > 0) {
  console.error(`✗ ${failures} 项校验失败`);
  process.exit(1);
}
console.log(`✓ ${checks} 项校验全部通过`);
