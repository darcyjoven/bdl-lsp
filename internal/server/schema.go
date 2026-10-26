package server

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/darcyjoven/bdl-lsp/index"
	"github.com/darcyjoven/bdl-lsp/internal/schema"
)

// schemaIndex 返回字段资料（可能为 nil：未启用、还没导入过、或资料坏了）。
func (s *Server) schemaIndex() *schema.Data {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.schema
}

// schemaLoadErr 返回上一次加载字段资料失败的原因（供视图显示）。
func (s *Server) schemaLoadErr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.schemaErr == nil {
		return ""
	}
	return s.schemaErr.Error()
}

// loadSchemaIndex 在 initialize 期间读取项目里的字段资料（P2-1）。
//
// 资料格式见 internal/schema：`.project/schema/tables.json` + 每表一个文件。
// 只读总索引（几十 KB），表文件在悬停/展开时才惰性读——所以这里很快。
//
// 三种结局都要能工作：关闭 → 不加载；没有资料 → 静默（新项目常态，
// 视图里会提示"还没导入"）；资料坏了 → 记下原因，视图里显示。
func (s *Server) loadSchemaIndex() {
	settings := s.settings()
	if !settings.SchemaEnabled() {
		s.logger.Info("字段资料已关闭（bdl.schema.enable=false）")
		s.mu.Lock()
		s.schema, s.schemaErr = nil, nil
		s.mu.Unlock()
		return
	}
	if s.rootPath == "" {
		return
	}
	// W6：新布局 `.project/schema/` 优先，回退历史 `.project/.schema/`。
	dir := schema.ResolveDataDir(s.rootPath, settings.SchemaDataDir())
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(s.rootPath, dir)
	}
	start := time.Now()
	ix, err := schema.LoadData(dir)
	if err != nil {
		s.mu.Lock()
		s.schema, s.schemaErr = nil, err
		s.mu.Unlock()
		if err == schema.ErrNoData {
			s.logger.Debug("还没有字段资料（可用 tiptop schema import 导入）", "dir", dir)
			return
		}
		s.logger.Warn("字段资料加载失败", "dir", dir, "error", err)
		return
	}
	s.mu.Lock()
	s.schema, s.schemaErr = ix, nil
	s.mu.Unlock()
	s.logger.Info("字段资料已加载",
		"dir", dir, "tables", ix.Len(), "importedAt", ix.ImportedAt,
		"ms", time.Since(start).Milliseconds())
}

// schemaTableFor 从声明的类型文本里取 `LIKE <表>.*` 的表名。
//
// 只认"整表展开"（`LIKE t.*`）：`LIKE t.col` 是单列声明，不是记录，
// 拿它的表去当成员集合会凭空多出成员。
func schemaTableFor(typeText string) string {
	up := strings.ToUpper(typeText)
	i := strings.Index(up, "LIKE")
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(typeText[i+len("LIKE"):])
	// 取到空白或行尾：`maj_file.*`
	if j := strings.IndexAny(rest, " \t\r\n,"); j >= 0 {
		rest = rest[:j]
	}
	if !strings.HasSuffix(rest, ".*") {
		return ""
	}
	name := strings.TrimSuffix(rest, ".*")
	if name == "" {
		return ""
	}
	return name
}

// schemaMembersForCompletion 返回 `.` 左侧记录变量在 schema 里的列。
//
// 只对 `RECORD LIKE t.*` 生效（`LIKE t.col` 是单列声明，没有成员）。
// 返回 nil 表示"没有更精确的信息"，补全退回尽力而为的旧行为。
func (s *Server) schemaMembersForCompletion(fi *index.FileIndex, lineText string) ([]schema.Column, string) {
	rec := qualifierBeforeDot(lineText)
	if rec == "" || fi == nil {
		return nil, ""
	}
	for _, sym := range fi.Symbols {
		if sym.Kind != index.KindRecord || !strings.EqualFold(sym.Name, rec) {
			continue
		}
		table := schemaTableFor(sym.TypeText)
		if table == "" {
			continue
		}
		tb, ok := s.schemaIndex().Table(table)
		if !ok || tb == nil || len(tb.Columns) == 0 {
			continue
		}
		return tb.Columns, table + " 的列（字段资料）"
	}
	return nil, ""
}

// schemaColumnsForType 从声明类型文本取"整表展开"的列：`LIKE t.*`。
func (s *Server) schemaColumnsForType(typeText string) (*schema.Table, bool) {
	table := schemaTableFor(typeText)
	if table == "" {
		return nil, false
	}
	return s.schemaIndex().Table(table)
}

// schemaColumnForType 取单列声明里的列：`LIKE t.col`。
//
// 与整表展开区分开：`LIKE t.col` 声明的是"与 t.col 同类型"的单值，
// 悬停时追加那一列的说明即可（不列整表）。
func (s *Server) schemaColumnForType(typeText string) (schema.Column, string, bool) {
	table, col, ok := schemaTableColumnFor(typeText)
	if !ok {
		return schema.Column{}, "", false
	}
	tb, found := s.schemaIndex().Table(table)
	if !found || tb == nil {
		return schema.Column{}, "", false
	}
	c, found := tb.Column(col)
	if !found {
		return schema.Column{}, "", false
	}
	return c, tb.Name, true
}

// schemaTableColumnFor 从类型文本里解析 `LIKE <表>.<列>`（单列）。
func schemaTableColumnFor(typeText string) (table, column string, ok bool) {
	up := strings.ToUpper(typeText)
	i := strings.Index(up, "LIKE")
	if i < 0 {
		return "", "", false
	}
	rest := strings.TrimSpace(typeText[i+len("LIKE"):])
	if j := strings.IndexAny(rest, " \t\r\n,"); j >= 0 {
		rest = rest[:j]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" || strings.HasSuffix(rest, ".*") {
		return "", "", false
	}
	dot := strings.LastIndexByte(rest, '.')
	if dot <= 0 || dot == len(rest)-1 {
		return "", "", false
	}
	return rest[:dot], rest[dot+1:], true
}

// qualifierBeforeDot 取 `.` 左侧的变量名：`maj.` → maj，`g_bxj[i].` → g_bxj，
// `x.ma` → x（用户已经打了一半成员名，`.` 在中间）。
func qualifierBeforeDot(line string) string {
	i := strings.LastIndexByte(line, '.')
	if i < 0 {
		return ""
	}
	rest := strings.TrimRight(line[:i], " \t")
	// 去掉数组下标后缀（可能多层）
	for strings.HasSuffix(rest, "]") {
		depth := 0
		open := -1
		for j := len(rest) - 1; j >= 0; j-- {
			switch rest[j] {
			case ']':
				depth++
			case '[':
				depth--
				if depth == 0 {
					open = j
				}
			}
			if open >= 0 {
				break
			}
		}
		if open <= 0 {
			return ""
		}
		rest = strings.TrimRight(rest[:open], " \t")
	}
	end := len(rest)
	j := end
	for j > 0 && isWordByte(rest[j-1]) {
		j--
	}
	return rest[j:end]
}

// isWordByte 判断是否是标识符字符（与补全的词法保持一致）。
func isWordByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}

// recordMembersOfType 用 schema 把 `LIKE t.*` 展开成成员集合。
//
// 返回 nil 表示"schema 里没有这张表/这张表没有列"——调用方按不可校验处理。
func (s *Server) recordMembersOfType(typeText string) map[string]bool {
	table := schemaTableFor(typeText)
	if table == "" {
		return nil
	}
	tb, ok := s.schemaIndex().Table(table)
	if !ok || tb == nil || len(tb.Columns) == 0 {
		return nil
	}
	members := make(map[string]bool, len(tb.Columns))
	for _, c := range tb.Columns {
		if c.Lower != "" {
			members[c.Lower] = true
		}
	}
	return members
}
