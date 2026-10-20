// 占位模块：让 `go build ./...` 跳过 artifacts/ 下的缓存与中间产物。
// 该目录只放过程文件（日志、缓存、语料结果），不包含产品代码。
module artifacts

go 1.24
