#!/usr/bin/env node
// 校验生成的 TextMate 语法能正确切分真实代码。
//
// 为什么需要这个校验：
//   VSCode 不对外暴露 TextMate token 化结果，无法在自动化里直接断言高亮。
//   但高亮的本质是"若干规则按顺序作用在文本上"，这里把同一套规则
//   按同一顺序跑一遍，就能验证：
//     - 关键字匹配到正确的 scope
//     - 多词关键字优先于单词前缀（END IF 不被切成 END + IF）
//     - 注释/字符串内部的内容不会被误判成关键字
//
// 语义细节（实测踩过，容易写错）：
//   - `match` 与 `begin` 在当前位置匹配（sticky）；
//   - `end` 是向前搜索，不是定位匹配；
//   - TextMate（Oniguruma）的 `$` 默认按行尾，JS 需要 m 标志；
//   - `(?i)` 行内标志 JS 不支持，需剥离后改用 i 标志。
//
// 用法：
//   node scripts/verify-tm-grammars.mjs
import * as fs from "node:fs";
import * as path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, "..", "..");
const syntaxDir = path.join(root, "vscode-extension", "syntaxes");

let failures = 0;
let checks = 0;

function fail(msg) {
  failures++;
  console.error(`  ✗ ${msg}`);
}

/** 把 TextMate 正则源码规范化：剥离 (?i)，返回 { source, flags }。 */
function normalize(pattern) {
  let src = pattern;
  let flags = "m";
  if (src.startsWith("(?i)")) {
    src = src.slice(4);
    flags += "i";
  }
  return { src, flags };
}

/** 在当前位置匹配（对应 TextMate 的 match / begin）。 */
function matchAt(pattern, text, index) {
  const { src, flags } = normalize(pattern);
  const re = new RegExp(src, flags + "y");
  re.lastIndex = index;
  const m = re.exec(text);
  return (m && m[0].length > 0 ? m : null);
}

/**
 * 把一次匹配按 captures 拆成多个 token。
 *
 * TextMate 里 `(FUNCTION)\s+(name)` 这类规则靠捕获组分别着色，
 * 未命中捕获组的字符不产生 token。只实现单层捕获组（本项目足够）。
 */
function splitCaptures(match, captures, baseOffset, fallbackScope) {
  if (!captures || Object.keys(captures).length === 0) {
    return [
      { start: baseOffset, text: match[0], scope: fallbackScope, top: true },
    ];
  }
  const parts = [];
  // 捕获组按 1..n 排序，丢弃 0（整段）以免与具体组重叠
  const keys = Object.keys(captures)
    .map(Number)
    .filter((n) => n > 0)
    .sort((a, b) => a - b);
  for (const n of keys) {
    const text = match[n];
    if (!text) {
      continue;
    }
    // 计算该组在原文本中的偏移
    const offset = baseOffset + match[0].indexOf(text);
    parts.push({
      start: offset,
      text,
      scope: captures[String(n)]?.name ?? fallbackScope,
      top: true,
    });
  }
  // 捕获组之外的部分也保留（scope 用外层 name），保证不丢内容
  if (parts.length === 0) {
    return [
      { start: baseOffset, text: match[0], scope: fallbackScope, top: true },
    ];
  }
  return parts;
}

/** 从指定位置向前搜索（对应 TextMate 的 end）。 */
function searchFrom(pattern, text, index) {
  const { src, flags } = normalize(pattern);
  const re = new RegExp(src, flags + "g");
  re.lastIndex = index;
  return re.exec(text);
}

/** 展开 include，得到有序规则数组。 */
function flatten(grammar, patterns) {
  const rules = [];
  for (const p of patterns ?? []) {
    if (p.include) {
      const key = p.include.replace(/^#/, "");
      const entry = grammar.repository[key];
      if (entry) {
        rules.push(...flatten(grammar, entry.patterns ?? [entry]));
      }
      continue;
    }
    if (p.match) {
      rules.push({
        kind: "match",
        pattern: p.match,
        scope: p.name,
        captures: p.captures,
      });
    } else if (p.begin && p.end) {
      rules.push({
        kind: "region",
        begin: p.begin,
        end: p.end,
        scope: p.name,
        inner: flatten(grammar, p.patterns),
      });
    }
  }
  return rules;
}

/**
 * 简化版 token 化。
 *
 * 与 TextMate 的差异：不实现"嵌套捕获组优先"等细节，
 * 只保留对本项目语法足够的顺序匹配。区间内部会单独再扫一遍，
 * 用于验证"字符串/注释里的关键字没有被提为顶层"。
 */
function tokenize(grammar, text) {
  const rules = flatten(grammar, grammar.patterns);
  const tokens = [];
  let i = 0;

  while (i < text.length) {
    let matched = false;

    for (const rule of rules) {
      if (rule.kind === "match") {
        const m = matchAt(rule.pattern, text, i);
        if (!m) {
          continue;
        }
        tokens.push(
          ...splitCaptures(m, rule.captures, i, rule.scope),
        );
        i += m[0].length;
        matched = true;
        break;
      }

      const b = matchAt(rule.begin, text, i);
      if (!b) {
        continue;
      }
      const bodyStart = i + b[0].length;
      const e = searchFrom(rule.end, text, bodyStart);
      const endIdx = e ? e.index + e[0].length : text.length;
      const whole = text.slice(i, endIdx);

      tokens.push({
        start: i,
        text: whole,
        scope: rule.scope,
        top: true,
        children: tokenizeWithin(rule.inner, whole, i),
      });
      i = endIdx;
      matched = true;
      break;
    }

    if (!matched) {
      i++;
    }
  }
  return tokens;
}

/** 在已知文本内跑内部规则（用于区间内层）。 */
function tokenizeWithin(rules, text, baseOffset) {
  const out = [];
  let i = 0;
  while (i < text.length) {
    let matched = false;
    for (const rule of rules) {
      if (rule.kind !== "match") {
        continue;
      }
      const m = matchAt(rule.pattern, text, i);
      if (!m) {
        continue;
      }
      out.push({
        start: baseOffset + i,
        text: m[0],
        scope: rule.scope,
        top: false,
      });
      i += m[0].length;
      matched = true;
      break;
    }
    if (!matched) {
      i++;
    }
  }
  return out;
}

/** 断言文本被识别为指定 scope 前缀。 */
function expectScope(tokens, snippet, scopePrefix, label) {
  checks++;
  const hit = tokens.find(
    (t) =>
      t.top &&
      t.text.toLowerCase() === snippet.toLowerCase() &&
      (t.scope ?? "").startsWith(scopePrefix),
  );
  if (hit) {
    return;
  }
  const got = tokens
    .filter((t) => t.top && t.text.toLowerCase() === snippet.toLowerCase())
    .map((t) => t.scope);
  fail(
    `${label}：期望 "${snippet}" → ${scopePrefix}*，实际 ${got.join(", ") || "（未匹配）"}`,
  );
}

/** 断言多词短语被整体匹配。 */
function expectPhrase(tokens, regex, label) {
  checks++;
  const hit = tokens.find((t) => t.top && regex.test(t.text));
  if (!hit) {
    fail(`${label}：多词短语未整体匹配（期望 /${regex.source}/）`);
  }
}

/** 断言区间内部的某词没有变成关键字级 scope。 */
function expectNoKeywordInside(token, snippet, label) {
  checks++;
  if (!token) {
    fail(`${label}：未找到区间 token`);
    return;
  }
  const bad = (token.children ?? []).find(
    (c) =>
      c.text.toLowerCase() === snippet.toLowerCase() &&
      /^keyword\.|^storage\.type/.test(c.scope ?? ""),
  );
  if (bad) {
    fail(`${label}：区间内部的 "${snippet}" 不应被识别为关键字`);
  }
}

// ---------------------------------------------------------------------------
// BDL
// ---------------------------------------------------------------------------

function verifyBDL() {
  const grammar = JSON.parse(
    fs.readFileSync(path.join(syntaxDir, "bdl.tmLanguage.json"), "utf8"),
  );
  console.log("BDL 语法");

  const source = [
    "-- 行注释里的 IF 不应高亮",
    "{ 块注释里的 END IF 也不应高亮 }",
    '&include "globals.4gl"',
    "&ifdef DEBUG",
    "SCHEMA ds",
    "DEFINE g_count LIKE type_file.num5",
    "TYPE t_rec RECORD",
    "    id INTEGER,",
    "    name VARCHAR(50)",
    "END RECORD",
    "",
    "MAIN",
    "    DEFINE l_rec t_rec",
    "    LET l_rec.id = 1",
    "    IF g_count > 0 THEN",
    '        DISPLAY "字符串里的 IF 不应高亮"',
    "        CALL show_name(12)",
    "    ELSE",
    "        LET g_count = 3.14",
    "    END IF",
    "    WHILE g_count < 10",
    "        LET g_count = g_count + 1",
    "    END WHILE",
    "END MAIN",
    "",
    "FUNCTION show_name(n)",
    "    DEFINE n INTEGER",
    "    RETURN n",
    "END FUNCTION",
    "",
    "REPORT r_list()",
    "    ORDER BY g_count",
    "    FORMAT",
    "        ON EVERY ROW",
    "            PRINT g_count",
    "END REPORT",
    "",
  ].join("\n");

  const tokens = tokenize(grammar, source);

  // 声明与类型
  expectScope(tokens, "MAIN", "storage.type.function", "MAIN");
  expectScope(tokens, "FUNCTION", "storage.type.function", "FUNCTION");
  expectScope(tokens, "DEFINE", "keyword.other.declaration", "DEFINE");
  expectScope(tokens, "RECORD", "keyword.other.declaration", "RECORD");
  expectScope(tokens, "INTEGER", "storage.type.bdl", "INTEGER");
  expectScope(tokens, "VARCHAR", "storage.type.bdl", "VARCHAR");

  // 控制流
  expectScope(tokens, "IF", "keyword.control", "IF");
  expectScope(tokens, "THEN", "keyword.control", "THEN");
  expectScope(tokens, "ELSE", "keyword.control", "ELSE");
  expectScope(tokens, "WHILE", "keyword.control", "WHILE");
  expectScope(tokens, "LET", "keyword.control", "LET");
  expectScope(tokens, "RETURN", "keyword.control", "RETURN");

  // 语句与报表
  expectScope(tokens, "DISPLAY", "keyword.other.statement", "DISPLAY");
  expectScope(tokens, "ORDER", "keyword.other.report", "ORDER");
  expectScope(tokens, "PRINT", "keyword.other.report", "PRINT");

  // 多词关键字整体匹配
  expectPhrase(tokens, /^END\s+MAIN$/i, "END MAIN");
  expectPhrase(tokens, /^END\s+FUNCTION$/i, "END FUNCTION");
  expectPhrase(tokens, /^END\s+IF$/i, "END IF");
  expectPhrase(tokens, /^END\s+WHILE$/i, "END WHILE");
  expectPhrase(tokens, /^END\s+RECORD$/i, "END RECORD");
  expectPhrase(tokens, /^END\s+REPORT$/i, "END REPORT");
  expectPhrase(tokens, /^ON\s+EVERY\s+ROW$/i, "ON EVERY ROW");

  // 注释区间
  checks++;
  const lineComment = tokens.find(
    (t) => t.top && /^comment\.line/.test(t.scope ?? ""),
  );
  if (!lineComment) {
    fail("没有识别出行注释");
  } else {
    expectNoKeywordInside(lineComment, "IF", "行注释");
  }

  checks++;
  const blockComment = tokens.find(
    (t) => t.top && /^comment\.block/.test(t.scope ?? ""),
  );
  if (!blockComment) {
    fail("没有识别出块注释");
  } else if (!blockComment.text.startsWith("{") || !blockComment.text.endsWith("}")) {
    fail(`块注释边界错误：${JSON.stringify(blockComment.text.slice(0, 40))}`);
  }

  // 字符串区间
  checks++;
  const allStrings = tokens.filter((t) => t.top && /^string\./.test(t.scope ?? ""));
  if (allStrings.length === 0) {
    fail("没有识别出字符串");
  } else {
    // 取正文里那个含 IF 的字符串（&include 的文件名不算）
    const str = allStrings.find((t) => t.text.includes("IF"));
    if (!str) {
      fail(
        `没有找到含关键字的字符串，实际字符串：${allStrings
          .map((t) => JSON.stringify(t.text))
          .join(", ")}`,
      );
    } else {
      if (!/^"字符串里的 IF 不应高亮"$/.test(str.text.trim())) {
        fail(`字符串边界不正确：${JSON.stringify(str.text)}`);
      }
      expectNoKeywordInside(str, "IF", "字符串");
    }
  }

  // 数字
  expectScope(tokens, "3.14", "constant.numeric.float", "浮点数");
  expectScope(tokens, "1", "constant.numeric.integer", "整数");

  // 预处理器
  checks++;
  const pp = tokens.filter((t) =>
    /^keyword\.control\.preprocessor/.test(t.scope ?? ""),
  );
  if (pp.length === 0) {
    fail("没有识别出预处理指令");
  } else {
    if (!pp.some((t) => /include/i.test(t.text))) {
      fail("&include 未识别");
    }
    if (!pp.some((t) => /ifdef/i.test(t.text))) {
      fail("&ifdef 未识别");
    }
  }

  // 函数名
  expectScope(tokens, "show_name", "entity.name.function", "函数名");

  // 字段访问
  checks++;
  const accessor = tokens.find(
    (t) => t.top && t.scope === "punctuation.accessor.bdl",
  );
  if (!accessor) {
    fail("没有识别出字段访问 .");
  }
}

// ---------------------------------------------------------------------------
// .per
// ---------------------------------------------------------------------------

function verifyPER() {
  const grammar = JSON.parse(
    fs.readFileSync(path.join(syntaxDir, "per.tmLanguage.json"), "utf8"),
  );
  console.log("\n.per 语法");

  const source = [
    "-- 表单注释",
    "SCHEMA ds",
    "",
    "LAYOUT",
    "GRID",
    "{",
    "  Customer number: [f01    ]",
    "}",
    "END",
    "END",
    "",
    "TABLES",
    "  customer",
    "END",
    "",
    "ATTRIBUTES",
    "  f01 = customer.cust_num, NOENTRY;",
    "  f02 = customer.cust_name;",
    "END",
    "",
    "INSTRUCTIONS",
    "  SCREEN RECORD sr_customer(customer.*)",
    "END",
    "",
  ].join("\n");

  const tokens = tokenize(grammar, source);

  expectScope(tokens, "SCHEMA", "keyword.other.section", "SCHEMA");
  expectScope(tokens, "LAYOUT", "keyword.other.section", "LAYOUT");
  expectScope(tokens, "ATTRIBUTES", "keyword.other.section", "ATTRIBUTES");
  expectScope(tokens, "INSTRUCTIONS", "keyword.other.section", "INSTRUCTIONS");
  expectScope(tokens, "GRID", "keyword.other.container", "GRID");
  expectScope(tokens, "RECORD", "keyword.other.directive", "RECORD");
  expectScope(tokens, "END", "keyword.other.section", "END");

  checks++;
  const comment = tokens.find(
    (t) => t.top && /^comment\./.test(t.scope ?? ""),
  );
  if (!comment) {
    fail("没有识别出 .per 注释");
  }

  checks++;
  const accessor = tokens.find(
    (t) => t.top && t.scope === "punctuation.accessor.per",
  );
  if (!accessor) {
    fail("没有识别出 .per 的字段访问 .");
  }
}

// ---------------------------------------------------------------------------

verifyBDL();
verifyPER();

console.log("");
if (failures > 0) {
  console.error(`✗ ${failures}/${checks} 项校验失败`);
  process.exit(1);
}
console.log(`✓ ${checks} 项校验全部通过`);
