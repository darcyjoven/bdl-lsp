package server

import (
	"strings"
	"time"

	"github.com/darcyjoven/bdl-lsp/internal/index"
	"github.com/darcyjoven/bdl-lsp/internal/schema"
)

// schemaIndex 返回数据库 schema 索引（可能为 nil：未启用或没有 .sch 文件）。
func (s *Server) schemaIndex() *schema.Index {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.schema
}

// loadSchemaIndex 在 initialize 期间解析工作区的 `.sch` 文件（P6）。
//
// 同步加载：3127 个文件约 110 ms。异步会让 didOpen 首轮诊断与后续诊断
// 用不同的规则（先不校验、后校验），用户会看到"打开时不报、改一下就报"。
func (s *Server) loadSchemaIndex() {
	settings := s.settings()
	if !settings.SchemaEnabled() {
		s.logger.Info("数据库 schema 索引已关闭（bdl.schema.enable=false）")
		return
	}
	if s.rootPath == "" {
		return
	}
	start := time.Now()
	ix := schema.LoadWorkspace(s.rootPath, settings.Link.ModuleRoots, settings.Schema.Paths)
	elapsed := time.Since(start)
	if ix.Len() == 0 {
		s.logger.Debug("没有找到 schema 文件", "root", s.rootPath)
		return
	}
	s.mu.Lock()
	s.schema = ix
	s.mu.Unlock()
	s.logger.Info("数据库 schema 索引就绪",
		"files", ix.Files, "tables", ix.Len(), "problems", len(ix.Problems),
		"ms", elapsed.Milliseconds())
	for _, p := range ix.Problems {
		s.logger.Warn("schema 解析可疑语句", "detail", p)
	}
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
func (s *Server) schemaMembersForCompletion(fi *index.FileIndex, lineText string) ([]string, string) {
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
		tb := s.schemaIndex().Table(table)
		if tb == nil || len(tb.Columns) == 0 {
			continue
		}
		cols := make([]string, 0, len(tb.Columns))
		for _, c := range tb.Columns {
			cols = append(cols, c.Name)
		}
		return cols, table + " 的列（schema）"
	}
	return nil, ""
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
	tb := s.schemaIndex().Table(table)
	if tb == nil || len(tb.Columns) == 0 {
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
