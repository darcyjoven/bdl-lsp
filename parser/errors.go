package parser

import "errors"

var (
	// ErrTimeout 表示解析超时。
	ErrTimeout = errors.New("解析超时")
	// ErrTooLarge 表示文件超过配置的大小或行数上限。
	ErrTooLarge = errors.New("文件超过解析上限")
)
