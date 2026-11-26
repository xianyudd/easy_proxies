# 订阅归属验证

管理员可以在订阅设置查看自动识别的名称，在节点总览和节点配置查看来源，并以订阅名搜索节点。

| 验收行为 | 验证 |
| --- | --- |
| 解析响应头，缺少可用名称时回退到域名 | `go test ./internal/config -run 'TestSubscription(ResponseName\|Names\|CacheKeeps)' -count=1` |
| 原有节点名保持兼容，缓存能保留归属，部分刷新不会改写其他订阅的名称 | 同一组配置测试，含共享代理入口的夹具 |
| 节点总览按订阅名搜索 | `TestHandleNodesPagedSearchAliasesFilterRows` |
| 设置页标签跟随已保存的 URL，编辑和重排时仍能正确匹配 | `node --test --experimental-test-coverage web/tests/subscriptionAttribution.test.mjs`，7 项通过，归属逻辑行覆盖率 100%，分支覆盖率 86.21% |

最初的订阅配置测试因 `SubscriptionNames` 和 `SubscriptionName` 尚不存在而编译失败。实现后，相关配置测试和前端测试均通过。独立实例使用本机模拟的订阅服务器验证设置和节点搜索，再关闭订阅响应并重启，确认仍可从缓存读取名称。此验证覆盖 API 和前端静态包交付。`go test ./...` 通过。配置包覆盖率为 65.9%，其中新增的 `subscription_names.go` 语句覆盖率为 94.1%（95/101）。既有前端静态脚本有 42 个基线失败，改动前后失败项一致。
