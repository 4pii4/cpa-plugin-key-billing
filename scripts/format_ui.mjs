import fs from "node:fs";
import assert from "node:assert/strict";
import { fileURLToPath, pathToFileURL } from "node:url";
import { isDeepStrictEqual } from "node:util";
import prettier from "prettier";
import { parse as parseJS } from "@babel/parser";
import postcss from "postcss";
import { parse as parseHTML } from "parse5";

// Repository-specific compact formatting. Install tooling with `npm ci --prefix scripts`.
// Prettier supplies syntax-aware layout; compaction preserves groups and is checked before writing.
const width = 140;
const options = { tabWidth: 2, objectWrap: "collapse", trailingComma: "none", quoteProps: "preserve" };
const preservedHTML = new Set(["pre", "textarea", "code", "xmp", "listing"]);
const walk = (node, visit) => {
  if (!node || typeof node !== "object") return;
  visit(node);
  for (const [key, value] of Object.entries(node)) {
    if (["tokens", "comments", "loc", "sourceCodeLocation", "parentNode"].includes(key)) continue;
    if (Array.isArray(value)) value.forEach((child) => walk(child, visit));
    else if (value && typeof value === "object") walk(value, visit);
  }
};
const applyEdits = (source, edits) => {
  edits.sort((a, b) => a.start - b.start || b.end - a.end);
  const selected = [];
  for (const edit of edits) {
    if (selected.length && edit.start < selected.at(-1).end) continue;
    selected.push(edit);
  }
  for (const edit of selected.reverse()) source = source.slice(0, edit.start) + edit.text + source.slice(edit.end);
  return source;
};
const fits = (source, start, end, text, limit) => {
  const prefix = source.slice(source.lastIndexOf("\n", start - 1) + 1, start);
  const lineEnd = source.indexOf("\n", end);
  const suffix = source.slice(end, lineEnd === -1 ? source.length : lineEnd);
  return !text.includes("\n") && prefix.length + text.length + suffix.length <= limit;
};
const codeAST = (source) => parseJS(source, { sourceType: "unambiguous", tokens: true });
const semanticJS = (source) =>
  JSON.parse(
    JSON.stringify(codeAST(source), (key, value) =>
      ["start", "end", "loc", "extra", "tokens", "comments", "leadingComments", "innerComments", "trailingComments"].includes(key)
        ? undefined
        : value
    )
  );

function compactJS(source, limit) {
  for (let pass = 0; pass < 20; pass++) {
    const next = alignCallArguments(compactJSPass(source, limit));
    if (next === source) return source;
    source = next;
  }
  throw new Error("JavaScript formatting did not converge; the file was not written");
}

function compactJSPass(source, limit) {
  const ast = codeAST(source);
  const edits = [];
  walk(ast.program, (node) => {
    if (!node.type || node.type === "Program" || node.type.startsWith("Comment")) return;
    if (!/(Statement|Declaration|Expression)$/.test(node.type) || node.type === "TemplateLiteral") return;
    const raw = source.slice(node.start, node.end);
    if (!raw.includes("\n") || /\n[ \t]*\n/.test(raw)) return;
    let hasMultipleStatements = false;
    walk(node, (child) => {
      if (child.type === "BlockStatement" && child.body.length > 1) hasMultipleStatements = true;
    });
    if (hasMultipleStatements) return;
    const tokens = ast.tokens.filter((token) => token.start >= node.start && token.end <= node.end);
    if (!tokens.length || tokens.some((token) => typeof token.type === "string" || source.slice(token.start, token.end).includes("\n")))
      return;
    let text = "";
    let previous;
    for (const token of tokens) {
      if (previous) {
        let gap = source.slice(previous.end, token.start);
        if (gap.includes("\n")) {
          gap = /^(\(|\[)$/.test(previous.type.label) || /^(\)|\]|;|,|\.|\?\.)$/.test(token.type.label) ? "" : " ";
        }
        text += gap;
      }
      text += source.slice(token.start, token.end);
      previous = token;
    }
    if (fits(source, node.start, node.end, text, limit)) edits.push({ start: node.start, end: node.end, text });
  });
  // Two joins that share a line must be checked in separate passes against the updated width.
  edits.sort((a, b) => a.start - b.start || b.end - a.end);
  let previousLineEnd = -1;
  const separateLines = edits.filter((edit) => {
    if (edit.start <= previousLineEnd) return false;
    previousLineEnd = source.indexOf("\n", edit.end);
    if (previousLineEnd < 0) previousLineEnd = source.length;
    return true;
  });
  return applyEdits(source, separateLines);
}

function alignCallArguments(source) {
  // A collapsed method chain loses one continuation level; its arguments must follow it.
  while (true) {
    const ast = codeAST(source);
    const edits = [];
    walk(ast.program, (node) => {
      if (node.type !== "CallExpression" && node.type !== "OptionalCallExpression") return;
      const opening = source.indexOf("(", node.callee.end);
      const firstBreak = source.indexOf("\n", opening);
      const closingLine = source.lastIndexOf("\n", node.end - 1) + 1;
      if (firstBreak < 0 || firstBreak >= closingLine) return;
      const openingLine = source.slice(source.lastIndexOf("\n", opening) + 1, opening);
      const closingPrefix = source.slice(closingLine, node.end - 1);
      if (!/^ *[})\]]*$/.test(closingPrefix)) return;
      const expected = openingLine.match(/^ */)[0].length + (/^ *[?:] /.test(openingLine) ? 2 : 0);
      const difference = closingPrefix.match(/^ */)[0].length - expected;
      if (difference <= 0) return;
      if (
        ast.tokens.some((token) => token.start > firstBreak && token.end < node.end && source.slice(token.start, token.end).includes("\n"))
      )
        return;
      const start = firstBreak + 1,
        end = node.end;
      const text = source.slice(start, end).replace(new RegExp("^ {" + difference + "}", "gm"), "");
      edits.push({ start, end, text });
    });
    if (!edits.length) return source;
    source = applyEdits(source, edits);
  }
}

function foldWhitespace(source) {
  // Only fold whitespace outside strings and comments; keep data URLs intact.
  return source.replace(
    /("(?:\\[\s\S]|[^"\\])*"|'(?:\\[\s\S]|[^'\\])*'|\/\*[\s\S]*?\*\/)|[ \t]*\n[ \t\n]*/g,
    (match, protectedText) => protectedText ?? " "
  );
}

function compactCSS(source, limit) {
  const root = postcss.parse(source);
  const edits = [];
  root.walk((node) => {
    if (!["rule", "decl"].includes(node.type)) return;
    if (node.type === "rule" && node.selector.includes("\n")) {
      const start = node.source.start.offset;
      const end = start + node.selector.length;
      const text = foldWhitespace(node.selector);
      if (fits(source, start, end, text, limit)) edits.push({ start, end, text });
    }
    if (node.type === "rule" && node.nodes.some((child) => child.type !== "decl")) return;
    const start = node.source.start.offset;
    const end = node.source.end.offset;
    const raw = source.slice(start, end);
    if (!raw.includes("\n") || /\n[ \t]*\n/.test(raw) || raw.includes("/*")) return;
    const text = foldWhitespace(raw);
    if (fits(source, start, end, text, limit)) edits.push({ start, end, text });
  });
  return applyEdits(source, edits);
}

function compactHTML(source) {
  const root = parseHTML(source, { sourceCodeLocationInfo: true });
  const edits = [];
  walk(root, (node) => {
    if (!node.tagName || ["script", "style"].includes(node.tagName) || insidePreservedHTML(node)) return;
    const location = node.sourceCodeLocation;
    if (!location) return;
    for (const range of [location, location.startTag]) {
      if (!range) continue;
      const start = range.startOffset;
      const end = range.endOffset;
      const raw = source.slice(start, end);
      if (!raw.includes("\n") || /\n[ \t]*\n/.test(raw) || /<!--|<(script|style|pre|textarea|code|xmp|listing)\b/i.test(raw)) continue;
      const text = foldWhitespace(raw);
      // Keep short footer labels together even when their unbreakable URL exceeds the guide width.
      const limit = node.tagName === "span" && node.parentNode?.tagName === "footer" ? 180 : width;
      if (fits(source, start, end, text, limit)) edits.push({ start, end, text });
    }
  });
  source = applyEdits(source, edits);
  const tagEdits = [];
  walk(parseHTML(source, { sourceCodeLocationInfo: true }), (node) => {
    if (insidePreservedHTML(node)) return;
    for (const range of [node.sourceCodeLocation?.startTag, node.sourceCodeLocation?.endTag]) {
      if (!range) continue;
      const start = range.startOffset,
        end = range.endOffset;
      const raw = source.slice(start, end);
      const text = raw.startsWith("</")
        ? raw.replace(/\s+(?=>)/g, "")
        : raw.replace(/("[^"]*"|'[^']*')|(?<=\S)[ \t]+(?=>)/g, (match, protectedText) => protectedText ?? "");
      tagEdits.push({ start, end, text });
    }
  });
  return applyEdits(source, tagEdits);
}

function insidePreservedHTML(node) {
  for (let current = node; current; current = current.parentNode) {
    if (preservedHTML.has(current.tagName)) return true;
  }
  return false;
}

function codeBlocks(source) {
  const blocks = [];
  walk(parseHTML(source, { sourceCodeLocationInfo: true }), (node) => {
    if (!["script", "style"].includes(node.tagName) || insidePreservedHTML(node)) return;
    const attributes = Object.fromEntries(node.attrs.map((attr) => [attr.name, attr.value]));
    if (
      node.tagName === "script" &&
      ("src" in attributes || !["", "module", "text/javascript", "application/javascript"].includes(attributes.type || ""))
    )
      return;
    if (node.tagName === "style" && attributes.type && attributes.type !== "text/css") return;
    const location = node.sourceCodeLocation;
    if (!location?.endTag) throw new Error(`Unclosed ${node.tagName} tag`);
    const start = location.startTag.endOffset,
      end = location.endTag.startOffset;
    const lineStart = source.lastIndexOf("\n", location.startOffset - 1) + 1;
    const tagIndent = source.slice(lineStart, location.startOffset).match(/^ */)[0];
    blocks.push({ type: node.tagName, start, end, tagIndent, content: source.slice(start, end) });
  });
  return blocks;
}

function literalRanges(source, type) {
  if (type === "script") {
    return codeAST(source)
      .tokens.filter((token) => ["string", "template", "regexp"].includes(token.type.label))
      .map((token) => ({ start: token.start, end: token.end }));
  }
  return [...source.matchAll(/\/\*[\s\S]*?\*\/|"(?:\\[\s\S]|[^"\\])*"|'(?:\\[\s\S]|[^'\\])*'/g)]
    .filter((match) => !match[0].startsWith("/*"))
    .map((match) => ({ start: match.index, end: match.index + match[0].length }));
}

function adjustIndent(source, type, remove, add) {
  const literals = literalRanges(source, type);
  let offset = 0;
  return source
    .split("\n")
    .map((line) => {
      const protectedLine = literals.some((range) => range.start < offset && range.end >= offset);
      offset += line.length + 1;
      if (!line || protectedLine) return line;
      return add + (line.startsWith(remove) ? line.slice(remove.length) : line);
    })
    .join("\n");
}

const semanticCSS = (source) => {
  const valueSyntax = (value) =>
    foldWhitespace(value).replace(
      /(\/\*[\s\S]*?\*\/)|("(?:\\[\s\S]|[^"\\])*"|'(?:\\[\s\S]|[^'\\])*')|\( +| +\)/g,
      (match, comment, literal) => comment ?? (literal ? JSON.stringify(literal.slice(1, -1)) : match.trim())
    );
  const result = [];
  postcss
    .parse(source)
    .walk((node) =>
      result.push({
        type: node.type,
        name: node.name,
        selector: node.selector && valueSyntax(node.selector),
        prop: node.prop,
        value: node.value && valueSyntax(node.value),
        params: node.params && valueSyntax(node.params),
        important: node.important,
        text: node.text,
        children: node.nodes?.length
      })
    );
  return result;
};
const semanticHTML = (source) => {
  const formattedBlocks = new Set(codeBlocks(source).map((block) => block.start));
  const simplify = (node, preserve = false) => {
    if (node.nodeName === "#text") return { text: preserve ? node.value : node.value.replace(/[\t\n\r\f ]+/g, " ") };
    return {
      name: node.nodeName,
      attrs: node.attrs,
      value: node.data,
      namespace: node.namespaceURI,
      publicId: node.publicId,
      systemId: node.systemId,
      children: formattedBlocks.has(node.sourceCodeLocation?.startTag?.endOffset)
        ? []
        : node.childNodes?.map((child) =>
            simplify(child, preserve || preservedHTML.has(node.tagName) || ["script", "style"].includes(node.tagName))
          )
    };
  };
  return simplify(parseHTML(source, { sourceCodeLocationInfo: true }));
};

export function assertEquivalent(before, after) {
  const beforeBlocks = codeBlocks(before),
    afterBlocks = codeBlocks(after);
  assert.equal(beforeBlocks.length, afterBlocks.length, "Inline code block count changed");
  for (let index = 0; index < beforeBlocks.length; index++) {
    const original = beforeBlocks[index],
      formatted = afterBlocks[index];
    assert.equal(original.type, formatted.type, "Inline code block type changed");
    if (original.type === "script") {
      assert(isDeepStrictEqual(semanticJS(original.content), semanticJS(formatted.content)), `Script ${index} AST changed`);
      assert(
        isDeepStrictEqual(
          codeAST(original.content).comments.map((comment) => comment.value),
          codeAST(formatted.content).comments.map((comment) => comment.value)
        ),
        "JavaScript comments changed"
      );
    } else {
      assert(isDeepStrictEqual(semanticCSS(original.content), semanticCSS(formatted.content)), `Stylesheet ${index} changed`);
    }
  }
  assert(isDeepStrictEqual(semanticHTML(before), semanticHTML(after)), "HTML changed");
}

export async function formatUI(before) {
  const edits = [];
  for (const block of codeBlocks(before)) {
    const indent = block.tagIndent + "  ",
      limit = width - indent.length;
    const source = adjustIndent(block.content.replace(/^\n|\n[ \t]*$/g, ""), block.type, indent, "");
    const formatted = await prettier.format(source, { ...options, printWidth: limit, parser: block.type === "script" ? "babel" : "css" });
    const compact = block.type === "script" ? compactJS(formatted, limit) : compactCSS(formatted, limit);
    const text = "\n" + adjustIndent(compact.trimEnd(), block.type, "", indent) + "\n" + block.tagIndent;
    edits.push({ start: block.start, end: block.end, text });
  }
  const after = compactHTML(applyEdits(before, edits));
  // Fail closed: never overwrite the source if a syntax tree or display text changes.
  assertEquivalent(before, after);
  return after;
}

async function main(args) {
  const usage =
    "Usage: node scripts/format_ui.mjs [--check] [file]\nDefault file: internal/plugin/ui.html (relative to this script, not the working directory)";
  if (args.length === 1 && ["--help", "-h"].includes(args[0])) {
    console.log(usage);
    return;
  }
  const check = args.includes("--check"),
    paths = args.filter((arg) => arg !== "--check");
  if (paths.length > 1 || paths.some((arg) => arg.startsWith("-"))) throw new Error(usage);
  const file = paths[0] ?? fileURLToPath(new URL("../internal/plugin/ui.html", import.meta.url));
  const before = fs.readFileSync(file, "utf8"),
    after = await formatUI(before);
  if (before === after) {
    console.log(`${file}: already formatted`);
  } else if (check) {
    console.error(`${file}: formatting required; run the formatter without --check`);
    process.exitCode = 1;
  } else {
    fs.writeFileSync(file, after);
    console.log(`${file}: formatted (${before.split("\n").length - 1} → ${after.split("\n").length - 1} lines)`);
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main(process.argv.slice(2)).catch((error) => {
    console.error(`UI formatting failed: ${error.message}`);
    process.exitCode = 1;
  });
}
