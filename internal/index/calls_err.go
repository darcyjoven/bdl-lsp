package index

import "errors"

// errNoFnCapture 表示 query 里找不到 @fn 捕获（query 文本与 grammar 脱节）。
var errNoFnCapture = errors.New("query 缺少 @fn 捕获")
