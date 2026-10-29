package server

// workspace/executeCommand 通道（PLAN Z-P3-2）。
//
// 为什么需要它：Zed 的扩展 API **没有发送 LSP 自定义请求的能力**
// （`zed_extension_api` 的 `Extension` trait 无 send_request），扩展只能通过
// 标准的 `workspace/executeCommand` 触发服务端能力。
//
// 设计：
//   - 命令名用 `bdl.<能力>`（点号），与既有的自定义请求 `bdl/<能力>` 一一对应；
//   - VSCode 侧**继续用**自定义请求（不受影响），两条路径共用底层实现
//     （`linkTreeFor` / 同一个 schema 索引 / 同一个强制解析入口），这里只做
//     "参数适配 + 转发"，不复制业务逻辑；
//   - 参数用位置数组（executeCommand 的 Arguments 是 []interface{}）：
//       bdl.linkTree            [uri]
//       bdl.linkReferences      [uri]
//       bdl.schemaTables        [query] 或 [query, limit]
//       bdl.schemaColumns       [table]
//       bdl.forceParse          [uri]
//       bdl.openLink            [目标文件 uri]（由 Code Lens 触发）
//
// 返回结构与对应自定义请求完全一致，客户端可以复用同一套解析代码。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

const (
	// CommandLinkTree 返回某个文件的链接视图（等价 bdl/linkTree）。arguments: [uri]
	CommandLinkTree = "bdl.linkTree"
	// CommandLinkReferences 只做反查（等价 bdl/linkReferences）。arguments: [uri]
	CommandLinkReferences = "bdl.linkReferences"
	// CommandSchemaTables 返回表清单（等价 bdl/schemaTables）。arguments: [query] 或 [query, limit]
	CommandSchemaTables = "bdl.schemaTables"
	// CommandSchemaColumns 返回单表列清单（等价 bdl/schemaColumns）。arguments: [table]
	CommandSchemaColumns = "bdl.schemaColumns"
	// CommandForceParse 强制解析当前文件（等价 bdl/forceParse）。arguments: [uri]
	CommandForceParse = "bdl.forceParse"
	// 注：CommandOpenLink 定义在 codelens.go（它是链接资料 Code Lens 的目标命令）。
)

// executeCommandIDs 返回 initialize 时声明给客户端的命令清单。
//
// 顺序即客户端命令选择器里的展示顺序。
func executeCommandIDs() []string {
	return []string{
		CommandLinkTree,
		CommandLinkReferences,
		CommandSchemaTables,
		CommandSchemaColumns,
		CommandForceParse,
		CommandOpenLink,
	}
}

// handleExecuteCommand 处理 `workspace/executeCommand`。
func (s *Server) handleExecuteCommand(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.ExecuteCommandParams
	if raw := req.Params(); len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return reply(ctx, nil, fmt.Errorf("解析 %s 参数: %w", protocol.MethodWorkspaceExecuteCommand, err))
		}
	}

	switch params.Command {
	case CommandLinkTree:
		uriStr, err := commandStringArg(params.Command, params.Arguments, 0, "uri")
		if err != nil {
			return reply(ctx, nil, err)
		}
		return reply(ctx, s.linkTreeFor(uriStr), nil)

	case CommandLinkReferences:
		uriStr, err := commandStringArg(params.Command, params.Arguments, 0, "uri")
		if err != nil {
			return reply(ctx, nil, err)
		}
		tree := s.linkTreeFor(uriStr)
		return reply(ctx, map[string]any{
			"refs":     tree.Refs,
			"refTotal": tree.RefTotal,
		}, nil)

	case CommandSchemaTables:
		var query string
		var limit int
		if len(params.Arguments) > 0 {
			q, err := commandStringArg(params.Command, params.Arguments, 0, "query（空串=不过滤）")
			if err != nil {
				return reply(ctx, nil, err)
			}
			query = q
		}
		if len(params.Arguments) > 1 {
			switch v := params.Arguments[1].(type) {
			case float64:
				limit = int(v)
			case int:
				limit = v
			default:
				return reply(ctx, nil, fmt.Errorf("%s: 第 2 个参数 limit 必须是数字", params.Command))
			}
		}
		settings := s.settings()
		res := schemaTablesResult{
			Enabled: settings.SchemaEnabled(),
			DataDir: s.schemaDataDir(),
		}
		ix := s.schemaIndex()
		if ix == nil {
			res.Error = s.schemaLoadErr()
			return reply(ctx, res, nil)
		}
		res.Present = true
		res.ImportedAt = ix.ImportedAt
		res.Source = ix.Source
		res.RowsTotal = ix.RowsTotal
		matched, total, ferr := ix.Search(query, limit)
		res.Total = total
		res.Matched = len(matched)
		if ferr != nil {
			res.FilterError = ferr.Error()
		}
		res.Returned = len(matched)
		res.Tables = make([]schemaTableInfo, 0, len(matched))
		for _, e := range matched {
			if e == nil {
				continue
			}
			res.Tables = append(res.Tables, schemaTableInfo{Name: e.Name, Desc: e.Desc, Columns: e.Columns})
		}
		return reply(ctx, res, nil)

	case CommandSchemaColumns:
		table, err := commandStringArg(params.Command, params.Arguments, 0, "table")
		if err != nil {
			return reply(ctx, nil, err)
		}
		settings := s.settings()
		res := schemaColumnsResult{Enabled: settings.SchemaEnabled(), Table: table}
		ix := s.schemaIndex()
		if ix == nil {
			res.Missing = true
			return reply(ctx, res, nil)
		}
		res.Present = true
		tb, ok := ix.Table(table)
		if !ok || tb == nil {
			res.Missing = true
			return reply(ctx, res, nil)
		}
		res.Table = tb.Name
		res.Desc = tb.Desc
		res.Columns = tb.Columns
		return reply(ctx, res, nil)

	case CommandOpenLink:
		uriStr, err := commandStringArg(params.Command, params.Arguments, 0, "uri")
		if err != nil {
			return reply(ctx, nil, err)
		}
		return s.openLink(ctx, reply, uriStr)

	case CommandForceParse:
		uriStr, err := commandStringArg(params.Command, params.Arguments, 0, "uri")
		if err != nil {
			return reply(ctx, nil, err)
		}
		// 与 bdl/forceParse 的语义一致：文件没打开就报错（编辑器据此提示）。
		if doc := s.docs.Get(uriStr); doc == nil {
			return reply(ctx, nil, fmt.Errorf("%s: 文件未打开：%s", MethodForceParse, uriStr))
		}
		s.setForceParse(uriStr, true)
		s.logger.Info("强制解析当前文件（executeCommand，后台，不限时）", "uri", uriStr)
		// 与自定义请求同一个入口：后台重解析，完成后发诊断 + bdl/parseDone。
		s.requestForceReparse(uriStr)
		return reply(ctx, nil, nil)

	default:
		return reply(ctx, nil, fmt.Errorf(
			"未知命令 %q（支持：%s）",
			params.Command, strings.Join(executeCommandIDs(), ", "),
		))
	}
}

// commandStringArg 取第 idx 个位置参数并断言是非空字符串。
func commandStringArg(command string, args []interface{}, idx int, what string) (string, error) {
	if len(args) <= idx {
		return "", fmt.Errorf("%s: 缺少第 %d 个参数 %s", command, idx+1, what)
	}
	str, ok := args[idx].(string)
	if !ok || str == "" {
		return "", fmt.Errorf("%s: 第 %d 个参数 %s 必须是非空字符串", command, idx+1, what)
	}
	return str, nil
}
