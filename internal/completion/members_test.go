package completion

import "testing"

// TestProvideAfterDotPrefersMembers 有精确成员时只用成员（不给表单字段/符号）。
func TestProvideAfterDotPrefersMembers(t *testing.T) {
	ctx := Context{
		Lang: "bdl", Line: "    LET rec.", AfterDot: true,
		Members:      []string{"apa01", "apa02"},
		MemberDetail: "apa_file 的列（schema）",
	}
	res := Provide(ctx, nil, nil, []string{"form_only"}, Options{})
	if len(res.Items) != 2 {
		t.Fatalf("候选数 = %d，期望 2：%+v", len(res.Items), res.Items)
	}
	for _, it := range res.Items {
		if it.Label == "form_only" {
			t.Errorf("有精确成员时不该混入表单字段")
		}
		if it.Detail != "apa_file 的列（schema）" {
			t.Errorf("Detail = %q", it.Detail)
		}
	}
}

// TestProvideAfterDotMembersFiltered 已有前缀时按前缀过滤。
func TestProvideAfterDotMembersFiltered(t *testing.T) {
	ctx := Context{
		Lang: "bdl", Line: "    LET rec.apa0", Word: "apa0", AfterDot: true,
		Members: []string{"apa01", "apa02", "other"},
	}
	res := Provide(ctx, nil, nil, nil, Options{})
	if len(res.Items) != 2 {
		t.Fatalf("候选数 = %d，期望 2（apa01/apa02）", len(res.Items))
	}
}

// TestProvideAfterDotNoMembersFallsBack 没有成员时退回旧行为。
func TestProvideAfterDotNoMembersFallsBack(t *testing.T) {
	ctx := Context{Lang: "bdl", Line: "    LET x = r.", AfterDot: true}
	res := Provide(ctx, nil, nil, []string{"f1"}, Options{})
	if len(res.Items) != 1 || res.Items[0].Label != "f1" {
		t.Fatalf("应退回表单字段，实际 %+v", res.Items)
	}
}
