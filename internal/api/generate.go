package api

// 本文件只承载代码生成指令；生成产物为 server.gen.go，请勿手工编辑。
//
// 执行方式（任选其一）：
//
//	make generate            # 推荐
//	go generate ./...        # 直接调用
//
// 前置条件：oapi-codegen 已作为工具依赖记录在 go.mod 中
//
//	go get -tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen
//
// 修改 API 的流程：编辑 api/openapi.yaml -> 重新生成 -> 处理编译错误。
// 生成代码中接口签名发生变化时，编译器会直接指出所有需要同步修改的 handler。
//
//go:generate go tool oapi-codegen -config cfg.yaml ../../api/openapi.yaml
