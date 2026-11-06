# 更新日志

本文件记录项目的重要变更。

## 未发布


## [0.2.1] - 2026-11-06

### 新增

- `Response[T]`：提供成功响应的数据、状态码、响应头与请求 ID。
- `HTTPError`：提供请求方法、已脱敏的 URL、响应头、Content-Type、请求 ID、`Retry-After`、原始错误 body 与截断标记。
- JSON 请求函数：`GetJSON`、`HeadJSON`、`OptionsJSON`、`PostJSON`、`PutJSON`、`PatchJSON`、`DeleteJSON` 与 `DoJSON`。
- `Client.Do`：适用于需要将非 2xx 转为错误的原始请求；`Client.DoRaw`：适用于需要自行检查任意状态码响应的场景。
- `WithBearerToken`、`WithBearerTokenProvider`、`WithDefaultHeader`、`WithQuery` 与 `WithAbsoluteURLs` 配置项。
- `WithRetryPolicy`：显式启用重试，并支持 `Retry-After`。
- `WithRequestHook`：观测每一次 HTTP 尝试，包括重试。
- 业务接入模板：`examples/example.go`，覆盖 GET、POST、PUT、DELETE、认证、重试和统一错误处理。

### 变更

- `NewClient` 现在返回 `(*Client, error)`，在创建阶段校验 base URL 和配置项。
- JSON 请求函数现在返回 `*Response[T]`，不再直接返回 `*T`。成功但没有响应 body（包括 `204 No Content`）时，`Response.Data` 为 `nil`。
- 相对 endpoint 即使以 `/` 开头也会保留 base URL 的路径。例如，`https://host/v1` 与 `/users` 会组合成 `https://host/v1/users`。
- 默认拒绝绝对 endpoint URL，避免请求意外绕过已配置的服务地址；仅可信目标可通过 `WithAbsoluteURLs` 显式开启。
- 仅在存在 JSON 请求 body 时设置 `Content-Type: application/json`，调用方可使用 `WithHeader` 覆盖。

### 修复

- 空的成功响应不再因 JSON 解码 `EOF` 失败。
- 对于非 2xx 的 HTML、JSON、纯文本响应，统一返回 `*HTTPError`，不再错误地尝试 JSON 解码。
- 响应中存在多个顶层 JSON 值时会返回解码错误。
- 传入空的自定义 HTTP 客户端或非法配置时，创建客户端会直接失败，不再在请求期间 panic。

### 安全

- 默认最多保留 64 KiB 的错误响应 body，可使用 `WithMaxErrorBodyBytes` 调整。
- `HTTPError.Error()` 不包含响应 body，减少敏感内容意外写入日志的风险。
- `HTTPError.URL` 会脱敏常见敏感 query 参数及 URL 用户信息。

### 迁移说明

1. 创建客户端时处理错误：

   ```go
   client, err := httpease.NewClient(baseURL)
   ```

2. 将旧的 `Get[T]`、`Post[T]`、`Put[T]`、`Delete[T]` 替换为对应的 `*JSON` 函数。

3. 从 `response.Data` 获取解码后的数据，并处理成功但 body 为空的情况：

   ```go
   response, err := httpease.GetJSON[User](ctx, client, "/users/42")
   if err != nil {
       return err
   }
   if response.Data == nil {
       return errors.New("用户响应为空")
   }
   user := *response.Data
   ```

4. 通过 `errors.As(err, &httpError)` 获取非 2xx 的结构化诊断信息；不要解析 `err.Error()` 文本。
