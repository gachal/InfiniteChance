# 26 素材读取韧性:content 端点云读失败回退行内地址

Type: grilling
Status: approved（2026-09-30 grilling 定案,待实现）

## Question

服务端出网故障时素材面整体瘫痪(2026-09-30 实证:重启电脑后 Docker 容器对外 TLS 全被 reset,`Storage.Open` 连续 EOF,content 端点 404,画布节点集体「素材不可用」;同一时刻宿主机网络正常、COS 对象完好可匿名 200、素材行 object_key/size_bytes 全部健在)。读路径如何对这类故障免疫?是否改为节点直存云公网地址(用户直觉中的「读取 obs 地址」)?

## Answer

1. **根因归档(环境,不在票内)**:该次故障是容器出网层问题(Docker Desktop/代理 TUN 重启后未恢复),写路径(转存 Put)与 gateway 上游访问同样受害,代码兜底救不了写入侧,环境修复(重启 Docker Desktop/代理)是唯一解。本票只解决**读路径**对这类故障的韧性。

2. **定案(方案 B):content 端点读兜底**。`internal/asset/handler.go` 的 `Content` 在 `a.ObjectKey != ""` 且 `Storage.Open` 返回**非 `fs.ErrNotExist` 错误**(网络/凭证/5xx 类传输错误)时回退:素材行 `url` 列为 http(s) 地址 → 预览 302 重定向到行内地址(浏览器走宿主机/用户网络直连,不经服务端出网);`?download=1` 走既有 `proxyLegacy` 代理,代理失败按现有「素材内容不可用」404 收尾。回退发生时 `log.Printf` 记素材 id 与 Open 错误(每请求一条,排障可见)。

3. **`fs.ErrNotExist` 不回退**:对象真删了(或行在对象不在)维持 404 占位语义——不把「对象没了」伪装成「网络坏了」,「素材被删显示占位」的既有契约(14 号票)不变。行 `url` 为空或 data: URI 时同样不回退,维持现状路径。

4. **否决方案 C(节点直存 `public_base_url + object_key`,前端直连 CDN)**:绝对地址进画布 JSON 后,换驱动/换桶/换公网基座的历史节点全体失效;救不了写入侧;与 14 号票「asset_id + 内容寻址相对路径」的既有语义冲突。留档备查,不再复议,除非出现「服务端长期不可达而 CDN 长期可达」的真实部署形态。

5. **302 安全边界不变**:重定向目标仍是该素材行自身存储的地址(`url` 列),非开放重定向——现有注释的论证对回退路径同样成立。

6. **验证点**:存量行中 `url` 为厂商临时地址(约 24h)的,回退在厂商地址过期后仍 404——由节点占位语义接住,不视为缺陷;302 后 `<img>/<video>` 跨域加载(云桶域名与画布不同源)对媒体元素无 CORS 阻碍(media 标签不加 crossorigin 即不受限),实测确认。

7. **产出面**:`internal/asset/handler.go`(Content 回退分支)、`handler_test.go`(Open 传输错误 → 302;Open `fs.ErrNotExist` → 404;download 回退代理;url 为空不回退);CONTEXT.md「素材」词条回写。
