#!/usr/bin/env node
// 从 tree-sitter grammar 源码生成 VSCode 的 TextMate 语法。
//
// 为什么生成而不是手写：
//   关键字近 400 个，手抄必然与 grammar 漂移。这里直接读 grammar 里的
//   `kw("...")`，grammar 更新后重跑即可同步；未分类的词会打印警告，
//   分类型表里拼错/过时的词也会打印警告。
//
// 为什么需要顺序校验：
//   TextMate 的 patterns 是"先匹配到先用"。"END" 若排在 "END IF" 之前，
//   `END IF` 会被切成 END + IF 两个关键字，高亮区域跟着错。
//   因此多词短语必须按长度降序整体排在前面的规则里，本脚本会校验这一点，
//   一旦有人加关键字破坏了顺序，生成直接失败而不是产出坏语法。
//
// 用法：
//   node scripts/generate-tm-grammars.mjs            # 生成
//   node scripts/generate-tm-grammars.mjs --check    # 只校验是否最新（CI 用）
import * as fs from "node:fs";
import * as path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "..");
const workspaceRoot = path.resolve(repoRoot, "..");

const BDL_GRAMMAR = path.join(workspaceRoot, "tree-sitter-bdl");
const PER_GRAMMAR = path.join(workspaceRoot, "tree-sitter-per");
const OUT_DIR = path.join(workspaceRoot, "vscode-extension", "syntaxes");

const checkOnly = process.argv.includes("--check");

// ---------------------------------------------------------------------------
// 关键字提取
// ---------------------------------------------------------------------------

/** 从若干 js 文件里收集 kw("...") 短语。 */
function collectKw(files) {
  const words = new Set();
  for (const file of files) {
    const src = fs.readFileSync(file, "utf8");
    for (const m of src.matchAll(/kw\(\s*["'`]([^"'`]+)["'`]/g)) {
      words.add(m[1].toUpperCase().replace(/\s+/g, " ").trim());
    }
  }
  return words;
}

/** BDL grammar 分散在 grammar.js + rule/*.js。 */
function bdlKeywords() {
  const dir = path.join(BDL_GRAMMAR, "rule");
  const files = [path.join(BDL_GRAMMAR, "grammar.js")];
  for (const name of fs.readdirSync(dir)) {
    if (name.endsWith(".js")) {
      files.push(path.join(dir, name));
    }
  }
  return collectKw(files);
}

/** .per grammar 的关键字集中在 rule/keywords.js。 */
function perKeywords() {
  return collectKw([path.join(PER_GRAMMAR, "rule", "keywords.js")]);
}

// ---------------------------------------------------------------------------
// 分类表
//
// 每一组对应一个 TextMate scope。组的先后顺序不影响正确性：
// 生成时会统一拆成"多词短语"与"单词"两层，再按长度排序并校验。
// ---------------------------------------------------------------------------

const BDL_GROUPS = {
  // 声明与结构
  "storage.type.function.bdl": ["MAIN", "FUNCTION", "REPORT", "PRIVATE", "PUBLIC"],
  "keyword.other.declaration.bdl": [
    "DEFINE", "CONSTANT", "TYPE", "RECORD", "END RECORD", "ARRAY", "OF",
    "LIKE", "GLOBALS", "SCHEMA", "DATABASE", "IMPORT", "OPTIONS",
    "DIMENSION", "ATTRIBUTE", "ATTRIBUTES", "EXTERNAL", "DYNAMIC", "EXTEND",
    "INOUT", "OUT",
  ],
  // 控制流
  "keyword.control.bdl": [
    "IF", "THEN", "ELSE", "END IF", "CASE", "WHEN", "OTHERWISE", "END CASE",
    "WHILE", "END WHILE", "FOR", "END FOR", "FOREACH", "END FOREACH",
    "RETURN", "RETURNING", "CONTINUE", "EXIT", "GOTO", "CALL", "RUN",
    "SLEEP", "LET", "INITIALIZE", "VALIDATE", "FREE", "LOCATE",
    "STEP", "NEXT", "PREVIOUS", "PRIOR", "FIRST", "LAST", "ABSOLUTE",
    "RELATIVE", "NEXT OPTION", "UP", "DOWN",
    "TRY", "CATCH", "END TRY", "RAISE", "WHENEVER", "DEFER",
  ],
  // 交互语句（DISPLAY / INPUT / MENU / DIALOG …）
  "keyword.other.statement.bdl": [
    "DISPLAY", "END DISPLAY", "INPUT", "END INPUT", "INPUT NO WRAP",
    "INPUT WRAP", "MENU", "END MENU", "BEFORE MENU", "CONSTRUCT",
    "END CONSTRUCT", "PROMPT", "END PROMPT", "DIALOG", "END DIALOG",
    "MESSAGE", "ERROR", "WARNING", "SHOW", "HELP", "HELP FILE",
    "OPEN FORM", "CLOSE FORM", "OPEN WINDOW", "CLOSE WINDOW",
    "CLEAR", "CLEAR WINDOW", "CURRENT WINDOW IS", "WINDOW", "SCREEN",
    "FORM", "FIELD", "COMMAND", "KEY", "ACCEPT", "CANCEL",
    "QUIT", "HIDE", "OPTION", "BY NAME", "NAME", "RUN IN", "IN FORM MODE",
    "IN LINE MODE", "INFIELD", "UNBUFFERED", "WITHOUT DEFAULTS",
    "LABEL", "AT", "ROW", "ROWS", "SCROLL", "MAXCOUNT",
    "INSERT ROW", "DELETE ROW", "APPEND ROW", "INSERT", "DELETE",
  ],
  // 交互事件（BEFORE/AFTER/ON …）
  "keyword.other.event.bdl": [
    "BEFORE CONSTRUCT", "AFTER CONSTRUCT", "BEFORE DELETE", "AFTER DELETE",
    "BEFORE DIALOG", "AFTER DIALOG", "BEFORE DISPLAY", "AFTER DISPLAY",
    "BEFORE FIELD", "AFTER FIELD", "BEFORE INPUT", "AFTER INPUT",
    "BEFORE INSERT", "AFTER INSERT", "BEFORE ROW", "AFTER ROW",
    "ON", "ON ACTION", "ON KEY", "ON CHANGE", "ON APPEND", "ON INSERT",
    "ON UPDATE", "ON DELETE", "ON EXPAND", "ON COLLAPSE", "ON DRAG_START",
    "ON DRAG_FINISHED", "ON DRAG_ENTER", "ON DRAG_OVER", "ON DROP",
    "ON FILL BUFFER", "ON IDLE", "ON CLOSE APPLICATION",
    "ON TERMINATE SIGNAL CALL", "ON LAST ROW", "ON ROW CHANGE",
    "ON EVERY ROW",
  ],
  // 报表
  "keyword.other.report.bdl": [
    "START REPORT", "FINISH REPORT", "TERMINATE REPORT", "OUTPUT TO REPORT",
    "PRINT", "PRINTX", "SKIP", "PAUSE", "NEED", "EVERY ROW",
    "PAGE LENGTH", "TOP MARGIN", "BOTTOM MARGIN", "LEFT MARGIN",
    "RIGHT MARGIN", "PAGE HEADER", "PAGE TRAILER", "FIRST PAGE HEADER",
    "BEFORE GROUP OF", "AFTER GROUP OF", "GROUP", "ORDER", "BY", "ASC",
    "DESC", "COUNT", "WHERE", "TOP OF PAGE", "OUTPUT", "PRINTER",
    "DESTINATION", "PIPE", "FORMAT", "LINE", "LINES", "COLUMN", "COLUMNS",
    "SPACE", "DELIMITER",
  ],
  // SQL 与事务
  "keyword.other.sql.bdl": [
    "SELECT", "UPDATE", "INTO", "CREATE", "ALTER", "DROP", "TRUNCATE",
    "RENAME", "INDEX", "TABLE", "SEQUENCE", "CURSOR", "DECLARE", "PREPARE",
    "EXECUTE", "EXECUTE IMMEDIATE", "FETCH", "OPEN", "CLOSE", "PUT",
    "FLUSH", "LOAD FROM", "UNLOAD TO", "COMMIT WORK", "ROLLBACK WORK",
    "BEGIN WORK", "SAVEPOINT", "RELEASE SAVEPOINT", "TO SAVEPOINT", "LOCK TABLE", "UNLOCK TABLE", "CONNECT TO",
    "DISCONNECT", "SET CONNECTION", "SET ISOLATION TO",
    "SET LOCK MODE TO", "SET EXPLAIN", "SQL", "END SQL", "SQLERROR",
    "SQL INTERRUPT", "NOT FOUND", "WITH HOLD",
    "WITH CONCURRENT TRANSACTION", "WITH REOPTIMIZATION", "DESCRIBE",
    "UNIQUE", "CONSTRAINED", "UNCONSTRAINED", "MEMORY",
    "THROUGH", "THRU", "FROM", "WITH", "COMMITTED READ",
    "REPEATABLE READ", "DIRTY READ", "EXCLUSIVE", "CURSOR STABILITY",
    "LAST COMMITTED", "RETAIN UPDATE LOCKS", "WITHOUT WAITING", "WAIT",
    "NOT WAIT", "DORMANT", "KEEP",
  ],
  // 数据类型
  "storage.type.bdl": [
    "INTEGER", "SMALLINT", "BIGINT", "TINYINT", "DECIMAL", "DEC", "MONEY",
    "FLOAT", "SMALLFLOAT", "CHAR", "CHARACTER", "VARCHAR", "STRING", "TEXT",
    "BYTE", "DATE", "DATETIME", "INTERVAL", "BOOLEAN", "IMAGE",
  ],
  // 日期时间限定词
  "keyword.other.datetime.bdl": [
    "YEAR", "MONTH", "DAY", "HOUR", "MINUTE", "SECOND", "FRACTION",
    "TODAY", "CURRENT",
  ],
  // 常量
  "constant.language.bdl": ["TRUE", "FALSE", "NULL", "USER", "ANY"],
  // 运算符（词形式）
  "keyword.operator.word.bdl": [
    "AND", "OR", "NOT", "IN", "MATCHES", "ESCAPE", "MOD", "USING",
    "CLIPPED", "SPACES", "IS NULL", "IS NOT NULL", "TO NULL",
    "SHORT CIRCUIT", "ASCII",
  ],
  // 显示颜色
  "support.constant.color.bdl": [
    "BLACK", "BLUE", "CYAN", "GREEN", "MAGENTA", "RED", "WHITE", "YELLOW",
  ],
  // 显示属性
  "support.constant.attribute.bdl": [
    "BOLD", "DIM", "UNDERLINE", "BLINK", "REVERSE", "INVISIBLE", "NORMAL",
    "BORDER", "STYLE", "WORDWRAP", "AUTO APPEND", "UNITS",
    "COMMENT", "COMMENT LINE", "ERROR LINE", "MESSAGE LINE", "MENU LINE",
    "PROMPT LINE", "FORM LINE", "FIELD ORDER", "FIELD ORDER FORM",
  ],
  // 杂项关键字
  "keyword.other.misc.bdl": [
    "END", "FGL", "TO", "OF", "AS", "OFF", "MODE", "FILE", "JAVA",
    "PROGRAM", "XML HANDLER", "INTERRUPT", "STOP", "OPTIONS",
  ],
};

// 由多个 kw() 组合而成的块结束短语。
//
// grammar 里写作 seq(kw("END"), kw("MAIN"))，单个 kw("END MAIN") 并不存在，
// 所以它们不会出现在关键字审计里；但高亮必须把它们当成整体，
// 否则 `END MAIN` 会被拆成 END + MAIN 两个独立关键字。
const BDL_COMPOSED = {
  "storage.type.function.bdl": ["END MAIN", "END FUNCTION", "END REPORT"],
};

const PER_GROUPS = {
  "keyword.other.section.per": [
    "SCHEMA", "DATABASE", "LAYOUT", "SCREEN", "TABLES", "ATTRIBUTES",
    "INSTRUCTIONS", "KEYS", "ACTION", "DEFAULTS", "TOPMENU", "TOOLBAR",
    "END",
  ],
  "keyword.other.container.per": [
    "VBOX", "HBOX", "GROUP", "FOLDER", "PAGE", "GRID", "SCROLLGRID",
    "TABLE", "TREE",
  ],
  "storage.type.item.per": [
    "AGGREGATE", "BUTTON", "BUTTONEDIT", "CANVAS", "CHECKBOX", "COMBOBOX",
    "DATEEDIT", "EDIT", "FIELD", "IMAGE", "LABEL", "PROGRESSBAR",
    "RADIOGROUP", "SLIDER", "SPINEDIT", "TEXTEDIT", "TIMEEDIT",
    "WEBCOMPONENT", "PHANTOM", "FORMONLY",
  ],
  "keyword.other.menu.per": ["COMMAND", "SEPARATOR", "ITEM"],
  "keyword.other.directive.per": ["RECORD", "DELIMITERS", "DEFAULT", "SAMPLE"],
  "keyword.operator.word.per": ["AND", "OR", "NOT", "LIKE", "WHERE", "BY"],
  "constant.language.per": ["TRUE", "FALSE", "NULL"],
  "keyword.other.misc.per": [
    "SIZE", "TITLE", "TYPE", "TO", "WITHOUT", "INPUT",
    "PIXELS", "POINTS", "LINES", "CHARACTERS", "COLUMNS",
  ],
  "keyword.other.datetime.per": [
    "YEAR", "MONTH", "DAY", "HOUR", "MINUTE", "SECOND", "FRACTION",
  ],
};

// ---------------------------------------------------------------------------
// TextMate 规则构造
// ---------------------------------------------------------------------------

/** 短语 → 正则片段：词内空白放宽为 \s+，其余字符转义。 */
function phraseRegex(phrase) {
  return phrase
    .split(/\s+/)
    .map((w) => w.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"))
    .join("\\s+");
}

/** 把组合短语并入分类表（同名 scope 的短语合并到同一组）。 */
function mergeGroups(base, extra) {
  const out = {};
  for (const [scope, phrases] of Object.entries(base)) {
    out[scope] = [...phrases];
  }
  for (const [scope, phrases] of Object.entries(extra)) {
    out[scope] = [...(out[scope] ?? []), ...phrases];
  }
  return out;
}

/** 长度降序（长的先匹配），同长度按字典序保证输出稳定。 */
function byLenDesc(a, b) {
  if (b.length !== a.length) {
    return b.length - a.length;
  }
  return a.localeCompare(b);
}

/**
 * 把分类表拆成"多词层"和"单字层"。
 *
 * 多词层按组内最长短语降序排列，整组作为一个 pattern（正则里也按长度降序），
 * 这样 `END IF` / `ON EVERY ROW` 永远先于 `END` / `ON` 命中。
 * 单字层之间长度相同，互不为前缀，顺序无关。
 */
function buildLayers(groups, label) {
  const multi = []; // { scope, phrases }
  const single = []; // { scope, phrases }

  for (const [scope, phrases] of Object.entries(groups)) {
    const m = [];
    const s = [];
    for (const p of new Set(phrases)) {
      (p.includes(" ") ? m : s).push(p);
    }
    if (m.length > 0) {
      multi.push({ scope, phrases: m.sort(byLenDesc) });
    }
    if (s.length > 0) {
      single.push({ scope, phrases: s.sort() });
    }
  }

  // 多词层整体按"本组最长短语"降序，保证跨组的前缀关系也正确
  multi.sort((a, b) => byLenDesc(a.phrases[0], b.phrases[0]));

  // 校验：排在后面的组的任何短语，不能是前面某组短语的前缀
  for (let i = 0; i < multi.length; i++) {
    for (let j = i + 1; j < multi.length; j++) {
      for (const later of multi[j].phrases) {
        for (const earlier of multi[i].phrases) {
          if (
            later.startsWith(earlier + " ") ||
            later === earlier ||
            earlier.startsWith(later + " ")
          ) {
            throw new Error(
              `${label} 多词关键字顺序冲突："${earlier}"（${multi[i].scope}）` +
                ` 与 "${later}"（${multi[j].scope}）互相为前缀，` +
                `请把它们放进同一组`,
            );
          }
        }
      }
    }
  }

  return { multi, single };
}

/** 一组短语 → 一条 TextMate match 规则。 */
function ruleFor(scope, phrases) {
  const alts = phrases.map(phraseRegex).join("|");
  return {
    name: scope,
    match: `(?i)\\b(?:${alts})\\b`,
  };
}

/** 生成 repository 里的关键字条目 + 顶层 include 列表。 */
function keywordRules(layers) {
  const repo = {};
  const includes = [];
  let n = 0;
  for (const group of [...layers.multi, ...layers.single]) {
    const key = `kw-${n++}`;
    repo[key] = ruleFor(group.scope, group.phrases);
    includes.push({ include: `#${key}` });
  }
  return { repo, includes };
}

/** 收集分类表覆盖情况，供生成时提示。 */
function audit(label, keywords, groups) {
  const known = new Set();
  for (const list of Object.values(groups)) {
    for (const w of list) {
      known.add(w.toUpperCase());
    }
  }
  const missing = [...keywords].filter((w) => !known.has(w)).sort();
  const extra = [...known].filter((w) => !keywords.has(w)).sort();
  if (missing.length > 0) {
    console.warn(
      `! ${label}：${missing.length} 个关键字未分类（不会高亮）：\n  ${missing.join(" ")}`,
    );
  }
  if (extra.length > 0) {
    console.warn(
      `! ${label}：${extra.length} 个词不在 grammar 中（分类表可能过时）：\n  ${extra.join(" ")}`,
    );
  }
  return missing.length === 0 && extra.length === 0;
}

// ---------------------------------------------------------------------------
// BDL 语法
// ---------------------------------------------------------------------------

function buildBDL() {
  const keywords = bdlKeywords();
  // 组合短语不参与审计（grammar 里没有对应的单个 kw），但要参与高亮
  audit("BDL", keywords, BDL_GROUPS);
  const groups = mergeGroups(BDL_GROUPS, BDL_COMPOSED);
  const layers = buildLayers(groups, "BDL");
  const kw = keywordRules(layers);

  const grammar = {
    $schema:
      "https://raw.githubusercontent.com/martinring/tmlanguage/master/tmlanguage.json",
    name: "Genero BDL",
    scopeName: "source.4gl",
    fileTypes: ["4gl", "global"],
    patterns: [
      { include: "#comments" },
      { include: "#preprocessor" },
      { include: "#strings" },
      { include: "#numbers" },
      { include: "#routine-definition" },
      // CALL/RUN 必须排在关键字层之前：否则 CALL 先被关键字规则吃掉，
      // 其后的函数名就落不到 entity.name.function 上。
      { include: "#call-keyword" },
      ...kw.includes,
      // 普通 name( 形式必须排在关键字层之后：否则 `IF (`、`WHILE (` 会被
      // 误判成函数调用。
      { include: "#call-generic" },
      { include: "#field-access" },
      { include: "#operators" },
    ],
    repository: {
      comments: {
        patterns: [
          {
            name: "comment.block.bdl",
            begin: "\\{",
            end: "\\}",
            beginCaptures: {
              0: { name: "punctuation.definition.comment.begin.bdl" },
            },
            endCaptures: {
              0: { name: "punctuation.definition.comment.end.bdl" },
            },
          },
          {
            name: "comment.line.double-dash.bdl",
            match: "--.*$",
            captures: { 0: { name: "punctuation.definition.comment.bdl" } },
          },
          {
            name: "comment.line.number-sign.bdl",
            match: "#.*$",
            captures: { 0: { name: "punctuation.definition.comment.bdl" } },
          },
        ],
      },
      preprocessor: {
        patterns: [
          {
            name: "meta.preprocessor.include.bdl",
            match: "^\\s*&(include)\\b\\s*(\"[^\"]*\"|'[^']*')",
            captures: {
              1: { name: "keyword.control.preprocessor.bdl" },
              2: { name: "string.quoted.double.bdl" },
            },
          },
          {
            name: "meta.preprocessor.define.bdl",
            match: "^\\s*&(define)\\b\\s*([A-Za-z_][A-Za-z0-9_]*)",
            captures: {
              1: { name: "keyword.control.preprocessor.bdl" },
              2: { name: "entity.name.function.preprocessor.bdl" },
            },
          },
          {
            // &ifdef/&ifndef/&elif/&else/&endif/&undef 与 &IF defined(...)
            name: "meta.preprocessor.conditional.bdl",
            match: "^\\s*&(ifdef|ifndef|elif|else|endif|undef|if|then)\\b",
            captures: { 1: { name: "keyword.control.preprocessor.bdl" } },
          },
        ],
      },
      strings: {
        patterns: [
          {
            name: "string.quoted.double.bdl",
            begin: '%?"',
            end: '"',
            patterns: [
              { name: "constant.character.escape.bdl", match: "\\\\." },
              { name: "string.quoted.double.embedded.bdl", match: '""' },
            ],
          },
          {
            name: "string.quoted.single.bdl",
            begin: "%?'",
            end: "'",
            patterns: [
              { name: "constant.character.escape.bdl", match: "\\\\." },
              { name: "string.quoted.single.embedded.bdl", match: "''" },
            ],
          },
        ],
      },
      numbers: {
        patterns: [
          {
            name: "constant.numeric.float.bdl",
            match:
              "(?<![A-Za-z0-9_])[+-]?(?:[0-9]+\\.[0-9]*|\\.[0-9]+)(?:[eE][+-]?[0-9]+)?",
          },
          {
            name: "constant.numeric.integer.bdl",
            match: "(?<![A-Za-z0-9_])[+-]?[0-9]+",
          },
        ],
      },
      "routine-definition": {
        patterns: [
          {
            name: "meta.function.bdl",
            match: "(?i)\\b(FUNCTION)\\s+([A-Za-z_][A-Za-z0-9_$]*)",
            captures: {
              1: { name: "storage.type.function.bdl" },
              2: { name: "entity.name.function.bdl" },
            },
          },
          {
            name: "meta.function.report.bdl",
            match: "(?i)\\b(REPORT)\\s+([A-Za-z_][A-Za-z0-9_$]*)",
            captures: {
              1: { name: "storage.type.function.bdl" },
              2: { name: "entity.name.function.bdl" },
            },
          },
        ],
      },
      "call-keyword": {
        patterns: [
          {
            // CALL / RUN 后面紧跟的名字是函数调用。
            // 这条必须排在关键字层之前（见 patterns 顺序说明）。
            match: "(?i)\\b(CALL|RUN)\\s+([A-Za-z_][A-Za-z0-9_$]*)",
            captures: {
              1: { name: "keyword.control.bdl" },
              2: { name: "entity.name.function.bdl" },
            },
          },
        ],
      },
      "call-generic": {
        patterns: [
          {
            // 普通函数调用：name( 。排在关键字层之后，避免 `IF (` 被误判。
            match: "\\b([A-Za-z_][A-Za-z0-9_$]*)(?=\\s*\\()",
            captures: { 1: { name: "entity.name.function.bdl" } },
          },
        ],
      },
      "field-access": {
        patterns: [
          {
            // rec.field / table.column / obj.*
            match:
              "\\b([A-Za-z_][A-Za-z0-9_$]*)(\\s*\\.\\s*)([A-Za-z_][A-Za-z0-9_$]*|\\*)",
            captures: {
              1: { name: "variable.other.object.bdl" },
              2: { name: "punctuation.accessor.bdl" },
              3: { name: "variable.other.property.bdl" },
            },
          },
        ],
      },
      operators: {
        patterns: [
          {
            name: "keyword.operator.comparison.bdl",
            match: "<=|>=|<>|!=|==|=|<|>",
          },
          {
            name: "keyword.operator.arithmetic.bdl",
            match: "\\*\\*|\\*|/|\\+|-",
          },
          { name: "punctuation.terminator.statement.bdl", match: ";" },
          { name: "punctuation.separator.bdl", match: "," },
          { name: "parens.bdl", match: "[()\\[\\]]" },
        ],
      },
      ...kw.repo,
    },
  };

  return grammar;
}

// ---------------------------------------------------------------------------
// .per 语法
// ---------------------------------------------------------------------------

function buildPER() {
  const keywords = perKeywords();
  audit("PER", keywords, PER_GROUPS);
  const layers = buildLayers(PER_GROUPS, "PER");
  const kw = keywordRules(layers);

  const grammar = {
    $schema:
      "https://raw.githubusercontent.com/martinring/tmlanguage/master/tmlanguage.json",
    name: "Genero Form",
    scopeName: "source.per",
    fileTypes: ["per"],
    patterns: [
      { include: "#comments" },
      { include: "#strings" },
      { include: "#numbers" },
      ...kw.includes,
      { include: "#attributes" },
      { include: "#field-access" },
      { include: "#operators" },
    ],
    repository: {
      comments: {
        patterns: [
          {
            name: "comment.line.double-dash.per",
            match: "--.*$",
            captures: { 0: { name: "punctuation.definition.comment.per" } },
          },
          {
            name: "comment.line.number-sign.per",
            match: "#.*$",
            captures: { 0: { name: "punctuation.definition.comment.per" } },
          },
          {
            // 旧表单文件尾部的 ▼---- 装饰线
            name: "comment.line.decoration.per",
            match: "^\\s*▼.*$",
          },
        ],
      },
      strings: {
        patterns: [
          {
            name: "string.quoted.double.per",
            begin: '%?"',
            end: '"',
            patterns: [
              { name: "constant.character.escape.per", match: "\\\\." },
              { name: "string.quoted.double.embedded.per", match: '""' },
            ],
          },
          {
            name: "string.quoted.single.per",
            begin: "%?'",
            end: "'",
            patterns: [
              { name: "constant.character.escape.per", match: "\\\\." },
            ],
          },
        ],
      },
      numbers: {
        patterns: [
          {
            name: "constant.numeric.float.per",
            match: "(?<![A-Za-z0-9_.])[+-]?[0-9]+\\.[0-9]+",
          },
          {
            name: "constant.numeric.integer.per",
            match: "(?<![A-Za-z0-9_.])[+-]?[0-9]+",
          },
        ],
      },
      attributes: {
        patterns: [
          {
            // 属性行: name = value（值已被前面的字符串/数字规则处理过）
            match: "^\\s*([A-Za-z_][A-Za-z0-9_.]*)(\\s*)(=)",
            captures: {
              1: { name: "entity.other.attribute-name.per" },
              3: { name: "keyword.operator.assignment.per" },
            },
          },
        ],
      },
      "field-access": {
        patterns: [
          {
            match:
              "\\b([A-Za-z_][A-Za-z0-9_]*)(\\s*\\.\\s*)([A-Za-z_][A-Za-z0-9_]*|\\*)",
            captures: {
              1: { name: "variable.other.object.per" },
              2: { name: "punctuation.accessor.per" },
              3: { name: "variable.other.property.per" },
            },
          },
        ],
      },
      operators: {
        patterns: [
          { name: "keyword.operator.comparison.per", match: "<=|>=|<>|!=|=|<|>" },
          { name: "punctuation.separator.per", match: "," },
          { name: "parens.per", match: "[()\\[\\]]" },
        ],
      },
      ...kw.repo,
    },
  };

  return grammar;
}

// ---------------------------------------------------------------------------
// 输出
// ---------------------------------------------------------------------------

function emit(file, grammar) {
  const text = JSON.stringify(grammar, null, 2) + "\n";
  const rel = path.relative(workspaceRoot, file);
  if (checkOnly) {
    const old = fs.existsSync(file) ? fs.readFileSync(file, "utf8") : "";
    if (old !== text) {
      console.error(`✗ ${rel} 不是最新，请重新生成`);
      return false;
    }
    console.log(`✓ ${rel} 已是最新`);
    return true;
  }
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, text, "utf8");
  console.log(`✓ 生成 ${rel}`);
  return true;
}

const ok =
  emit(path.join(OUT_DIR, "bdl.tmLanguage.json"), buildBDL()) &&
  emit(path.join(OUT_DIR, "per.tmLanguage.json"), buildPER());

if (!ok) {
  process.exit(1);
}
