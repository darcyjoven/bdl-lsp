#!/usr/bin/env node
// check-sh-utf8-vars.mjs —— 扫描 shell 脚本里「变量引用紧跟非 ASCII 字节」的写法。
//
// 为什么需要它：
//   bash 判断"变量名到哪里结束"用的是 libc 的 isalnum()，而 isalnum() 是**按当前
//   locale 的 ctype 表**判定单字节的（bash 3.2 与 5.x 都是这样，见 general.h 的
//   legal_variable_char）。在某些 locale 下（例如把首字节 EF 认成字母的 8 位 ctype），
//   `$MODE（` 里的第一个字节 EF 会被吞进变量名，变成查找 `MODE\xEF`：
//     - 开了 set -u → "MODE<乱码>: unbound variable"，脚本直接退出；
//     - 没开 set -u → 变量展开成空，中文标点被吃掉，输出静默出错。
//   实测：bash 3.2 与 bash 5.2 在 en_US.ISO-8859-1 下都复现（macOS 的 ctype 就是这一类）。
//   写法改成 `${MODE}（` 就与 locale 无关（`}` 是 ASCII，扫描必然停下）。
//
// 用法：
//   node scripts/check-sh-utf8-vars.mjs [目录或文件 ...]   # 默认扫本仓库
//   退出码 0 = 干净；1 = 有需要改成 ${VAR} 的地方。

import fs from "node:fs";
import path from "node:path";

const roots = process.argv.slice(2);
if (roots.length === 0) {
  roots.push(path.resolve(path.dirname(new URL(import.meta.url).pathname), ".."));
}

const SHELL_EXT = /\.(sh|bash)$/i;
const SHEBANG = /^#!.*\b(bash|sh|dash|ksh|zsh)\b/;

/** 变量引用：跳过被转义的 \$；未加花括号的 $VAR / $1 / $@ 等。 */
const VARREF = /(?<!\\)\$(?:[A-Za-z_][A-Za-z0-9_]*|[0-9]+|[@*#?$!-])/g;

// 构建/产物目录不参与扫描（里面可能塞了第三方源码树、socket、日志）。
const SKIP_DIRS = new Set([
  ".git",
  "node_modules",
  "artifacts",
  "dist-vsix",
  "dist",
  "out",
  "coverage",
  "vendor",
  ".cache",
]);

function collect(target, out) {
  const st = fs.statSync(target);
  if (st.isDirectory()) {
    for (const name of fs.readdirSync(target)) {
      if (SKIP_DIRS.has(name)) {
        continue;
      }
      collect(path.join(target, name), out);
    }
    return;
  }
  if (!st.isFile()) {
    return; // socket / FIFO 等
  }
  if (!SHELL_EXT.test(target)) {
    const head = fs.readFileSync(target, "latin1").split("\n", 1)[0] ?? "";
    if (!SHEBANG.test(head)) {
      return;
    }
  }
  out.push(target);
}

const files = [];
for (const r of roots) {
  collect(path.resolve(r), files);
}

/**
 * 把一行里「会做参数展开」的部分留下，其余（注释、单引号串、反斜杠转义）用空格顶掉。
 * 用空格而不是删除，是为了保持列号不变；同一行是双引号串时仍会展开，所以要留。
 * 局限：不跟踪 heredoc（`<<'EOF'` 的正文若出现 `$VAR（` 会误报，加 ${} 即可，无害）。
 */
function codePart(line) {
  let inSingle = false;
  let inDouble = false;
  let out = "";
  for (let i = 0; i < line.length; i++) {
    const ch = line[i];
    if (inSingle) {
      out += ch === "'" ? "'" : " ";
      if (ch === "'") {
        inSingle = false;
      }
      continue;
    }
    if (ch === "\\") {
      out += "  "; // 转义的 \$ 不展开
      i++;
      continue;
    }
    if (ch === "'") {
      inSingle = true;
      out += ch;
      continue;
    }
    if (ch === '"') {
      inDouble = !inDouble;
      out += ch;
      continue;
    }
    if (ch === "#" && !inDouble) {
      break; // 注释到行尾
    }
    out += ch;
  }
  return out;
}

let bad = 0;
for (const file of files.sort()) {
  // 用 latin1 读：一个字节 == 一个字符，可以直接判断 >= 0x80。
  const text = fs.readFileSync(file, "latin1");
  const lines = text.split("\n");
  for (let i = 0; i < lines.length; i++) {
    const code = codePart(lines[i]);
    VARREF.lastIndex = 0;
    let m;
    while ((m = VARREF.exec(code)) !== null) {
      const at = m.index + m[0].length;
      if (at < code.length && code.charCodeAt(at) > 0x7f) {
        const rel = path.relative(process.cwd(), file);
        const bytes = [];
        for (let k = at; k < code.length && bytes.length < 3; k++) {
          if (code.charCodeAt(k) > 0x7f) {
            bytes.push(code.charCodeAt(k).toString(16).toUpperCase().padStart(2, "0"));
          } else {
            break;
          }
        }
        console.error(
          `${rel}:${i + 1}: 变量 ${m[0]} 紧跟非 ASCII 字节（0x${bytes.join(" 0x")}）→ 请写成 \${...}`,
        );
        bad++;
        break; // 一行只报第一处，够定位
      }
    }
  }
}

if (bad > 0) {
  console.error(`\n发现 ${bad} 处「变量紧跟非 ASCII」；改成 \${VAR} 即可（macOS/8 位 ctype 下会吞字节）。`);
  process.exit(1);
}
console.log(`check-sh-utf8-vars: ${files.length} 个 shell 脚本，未发现「变量紧跟非 ASCII」。`);