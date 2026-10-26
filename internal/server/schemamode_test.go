package server

import (
	"testing"
	"time"
)

// §0d：插件改用 mode 三态（off/file/db）下发配置——服务端必须认。
func TestSchemaModeConfig(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root := newSchemaWorkspace(t)
	h.initWS(root)
	h.waitIndex()

	// mode=db：即使没有旧的 enable，也应启用并持有资料
	h.send(`{"jsonrpc":"2.0","method":"workspace/didChangeConfiguration","params":{"settings":{"schema":{"mode":"db"}}}}`)
	time.Sleep(200 * time.Millisecond)
	resp := h.request(nextRequestID(), MethodSchemaTables, `{}`)
	res, _ := resp["result"].(map[string]any)
	if enabled, _ := res["enabled"].(bool); !enabled {
		t.Fatalf("mode=db 时 enabled 应为 true：%v", res)
	}
	if present, _ := res["present"].(bool); !present {
		t.Fatalf("mode=db 时应持有资料：%v", res)
	}

	// mode=off：关闭
	h.send(`{"jsonrpc":"2.0","method":"workspace/didChangeConfiguration","params":{"settings":{"schema":{"mode":"off"}}}}`)
	time.Sleep(200 * time.Millisecond)
	resp = h.request(nextRequestID(), MethodSchemaTables, `{}`)
	res, _ = resp["result"].(map[string]any)
	if enabled, _ := res["enabled"].(bool); enabled {
		t.Fatalf("mode=off 时 enabled 应为 false：%v", res)
	}

	// mode 优先于旧 enable：显式 off 之后再下发 enable=true 也不该复活
	//（旧键兼容只在"从未设过 mode"的配置里生效，见 config.TestModeTakesPrecedenceOverEnable）
	h.send(`{"jsonrpc":"2.0","method":"workspace/didChangeConfiguration","params":{"settings":{"schema":{"enable":true}}}}`)
	time.Sleep(200 * time.Millisecond)
	resp = h.request(nextRequestID(), MethodSchemaTables, `{}`)
	res, _ = resp["result"].(map[string]any)
	if enabled, _ := res["enabled"].(bool); enabled {
		t.Fatalf("mode=off 应优先于旧 enable=true：%v", res)
	}
}
