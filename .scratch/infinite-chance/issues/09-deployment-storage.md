# 09 部署、配置与素材存储

Type: grilling
Status: open
Blocked by: 05, 08

## Question

部署与素材存储如何定?

- docker-compose 形态:gateway/server、canvas/server、admin-web(静态资源如何 serve——nginx?并入 canvas/server?)、mysql、redis、对象存储(MinIO?)共几个服务
- 配置管理:env vs 配置文件;密钥(各厂商 API key、管理后台密码)如何管理
- 素材存储选型:本地磁盘 vs MinIO vs 云 OSS;图片/视频的上传下载链路与 URL 签名
- 端口与路由规划:网关 API、管理 API、画布 API、前端静态资源的路径/域名划分
- 备份与数据卷约定

## Answer
