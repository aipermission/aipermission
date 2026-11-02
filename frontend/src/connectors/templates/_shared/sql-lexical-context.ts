export type SQLContextToken = {
  text: string;
  start: number;
  end: number;
  kind: "identifier" | "literal" | "symbol";
  quoted: boolean;
  complete: boolean;
};

// Editor context only, never a SQL execution or permission validator.
export function sqlContextTokens(sql: string): SQLContextToken[] {
  const tokens: SQLContextToken[] = [];
  let index = 0;
  while (index < sql.length) {
    if (/\s/.test(sql[index])) {
      index++;
      continue;
    }
    if (sql.startsWith("--", index)) {
      while (index < sql.length && !/[\r\n]/.test(sql[index])) index++;
      continue;
    }
    if (sql.startsWith("/*", index)) {
      index += 2;
      let depth = 1;
      while (index < sql.length && depth) {
        if (sql.startsWith("/*", index)) {
          depth++;
          index += 2;
        } else if (sql.startsWith("*/", index)) {
          depth--;
          index += 2;
        } else index++;
      }
      continue;
    }
    const start = index;
    const opener = sql[index++];
    const quoted = opener === '"' || opener === "`" || opener === "[";
    const literal = opener === "'";
    let complete = true;
    if (quoted || literal) {
      const closer = opener === "[" ? "]" : opener;
      complete = false;
      while (index < sql.length) {
        if (sql[index++] !== closer) continue;
        if (sql[index] === closer) index++;
        else {
          complete = true;
          break;
        }
      }
    } else if (/[a-zA-Z_]/.test(opener)) {
      while (index < sql.length && /[\w$]/.test(sql[index])) index++;
    }
    tokens.push({
      text: sql.slice(start, index),
      start,
      end: index,
      quoted,
      complete,
      kind: quoted || /[a-zA-Z_]/.test(opener) ? "identifier" : literal ? "literal" : "symbol",
    });
  }
  return tokens;
}

export function sqlKeyword(token: SQLContextToken | undefined, keyword: string): boolean {
  return Boolean(token && !token.quoted && token.kind === "identifier" && token.text.toLowerCase() === keyword);
}
