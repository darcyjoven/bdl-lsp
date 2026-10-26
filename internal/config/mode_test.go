package config

import "testing"

// §0d：插件改用 mode 三态；mode 必须优先于旧的 enable。
func TestModeTakesPrecedenceOverEnable(t *testing.T) {
	no := false
	yes := true

	if got := (Settings{Schema: SchemaSettings{Enable: &no, Mode: "db"}}).SchemaEnabled(); !got {
		t.Error("schema mode=db 应启用（即使旧的 enable=false）")
	}
	if got := (Settings{Schema: SchemaSettings{Enable: &yes, Mode: "off"}}).SchemaEnabled(); got {
		t.Error("schema mode=off 应关闭（即使旧的 enable=true）")
	}
	if got := (Settings{Schema: SchemaSettings{Mode: "file"}}).SchemaEnabled(); !got {
		t.Error("schema mode=file 应启用（只用 CSV 导入也要读资料）")
	}
	// 没给 mode：沿用旧 enable；都没有则按服务器默认（启用）
	if got := (Settings{Schema: SchemaSettings{Enable: &no}}).SchemaEnabled(); got {
		t.Error("没有 mode 时应沿用 enable=false")
	}
	if got := (Settings{Schema: SchemaSettings{}}).SchemaEnabled(); !got {
		t.Error("都没有时应按默认启用")
	}

	if got := (Settings{Link: LinkSettings{Enable: &no, Mode: "db"}}).LinkEnabled(); !got {
		t.Error("link mode=db 应启用（即使旧的 enable=false）")
	}
	if got := (Settings{Link: LinkSettings{Enable: &yes, Mode: "off"}}).LinkEnabled(); got {
		t.Error("link mode=off 应关闭（即使旧的 enable=true）")
	}
	if got := (Settings{Link: LinkSettings{Mode: "FILE"}}).LinkEnabled(); !got {
		t.Error("mode 大小写不敏感")
	}
}
