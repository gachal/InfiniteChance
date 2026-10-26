// Package voice stores the 音色台账 (57 号票):定制音色作为全局创作资产
// (同 characters 的创作资产语义,但跨 IP 复用、量小好管理 —— 不带
// ip_id)。台账行由画布任务的 worker 在上游音色部署完成时落库(kind=voice
// 的 FinalizeVoiceSuccess 同事务写入,本包的 Create 即为该路径服务);
// 台词合成(kind=tts)与角色绑定都从这里取渠道与上游句柄。
//
// 行的生命周期跟随上游:DEPLOYING → OK(可用)/ UNDEPLOYED(上游审核未
// 通过,不可用)。UNDEPLOYED 的行不落台账(worker 直接失败任务并丢弃
// 句柄),台账里只会有 OK 行 —— status 字段保留上游三态是为对账与将来
// 的 query 校准留位。删除语义:先经网关 delete_voice 成功(handler 职责)
// 再删行;被 characters.voice_id 引用时拒绝(ErrVoiceInUse —— 解绑后才
// 可删,不级联解绑)。
package voice

import (
	"context"
	"errors"
	"time"
)

// 上游音色的生命周期三态(56 号票契约,原样保留英文态字)。
const (
	StatusDeploying  = "DEPLOYING"
	StatusOK         = "OK"
	StatusUndeployed = "UNDEPLOYED"
)

// 音色的两种来源模式(56 号票 create 契约):design = 文本描述设计,
// clone = 声音样本克隆。
const (
	SourceModeDesign = "design"
	SourceModeClone  = "clone"
)

var (
	// ErrVoiceNotFound reports a voice id that has no row.
	ErrVoiceNotFound = errors.New("voice: voice not found")
	// ErrVoiceInUse reports Delete against a voice still bound by at least
	// one character(409 voice_in_use —— 解绑后才可删)。
	ErrVoiceInUse = errors.New("voice: voice is still bound by a character")
)

// Voice is one ledger row of a deployed custom voice. VoiceID 是上游的
// voice_id(渠道工作空间内唯一,库内唯一索引);ChannelID 是钉定的渠道
// (音色绑定创建渠道的工作空间,合成与删除都必须回同一渠道);SourcePrompt
// 在 design 模式存音色描述、clone 模式存样本地址(出处审计);Sample/
// PreviewAssetID 是素材行引用(0 = 无,预览素材在崩溃恢复路径可能缺)。
type Voice struct {
	ID             int64
	VoiceID        string
	ChannelID      int64
	TargetModel    string
	Name           string
	SourceMode     string
	SourcePrompt   string
	SampleAssetID  int64
	PreviewAssetID int64
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CreateInput carries Create's fields —— worker 落台账的行工厂
// (FinalizeVoiceSuccess 的事务内写入用同一组列)。Status 由调用方给
// (finalize 路径恒 OK);Name/TargetModel/VoiceID 非空由调用方保证,
// 本包不再校验(单管理员,写入点唯一)。
type CreateInput struct {
	VoiceID        string
	ChannelID      int64
	TargetModel    string
	Name           string
	SourceMode     string
	SourcePrompt   string
	SampleAssetID  int64
	PreviewAssetID int64
	Status         string
}

// VoiceUpdate carries Update's partial fields: nil = 不动。上游无改名
// API,本地 name 是台账的显示名(56 号票口径:不代理 update_voice)。
type VoiceUpdate struct {
	Name *string
}

// UpstreamError carries one gateway-answered rejection with its HTTP status
// and error summary intact —— DELETE 的透传契约(网关侧失败原样透传状态
// 与错误摘要)与 worker 轮询的暂态/永久判别(4xx 永久、5xx 暂态)共用。
// canvastask.Client 构造它,本包只定义形状(避免反向依赖)。
type UpstreamError struct {
	Status  int
	Message string
}

func (e *UpstreamError) Error() string { return e.Message }

// UpstreamRemover is the slice of the gateway client the DELETE handler
// needs: the upstream delete must succeed before the ledger row goes.
// *canvastask.Client satisfies it; nil Gateway = 未配置服务 key,删除
// 以 gateway_unconfigured 拒绝。
type UpstreamRemover interface {
	// DeleteVoice removes the upstream voice in its pinned channel. A
	// gateway-answered rejection arrives as *UpstreamError.
	DeleteVoice(ctx context.Context, channelID int64, voiceID, source string) error
}

// UpstreamQuerier is the slice of the gateway client the character binding
// calibration needs (57 号票评审 F1):绑定/换绑前经网关 query_voice 实时
// 校准,台账行的 status 只反映最后一次校准,不是实时事实。同一接口被
// worker 轮询与角色绑定共用;*canvastask.Client satisfies it。
type UpstreamQuerier interface {
	// QueryVoice asks the upstream lifecycle status of one voice in its
	// pinned channel. A gateway-answered rejection arrives as *UpstreamError.
	QueryVoice(ctx context.Context, channelID int64, voiceID, source string) (string, error)
}

// Store persists 音色台账. 全局作用域(无 ip_id):List 不带过滤,排序
// updated_at DESC(最近活动在前,与角色卡片墙同一哲学)。Delete 的
// 被引检查跨读 characters 表(同库,canvastask 读 assets 的同款先例)。
type Store interface {
	// Create inserts one ledger row (finalize 路径的行工厂);唯一索引
	// 冲突(同 voice_id 重复落行)按原样报错 —— 写入点唯一,重复即 bug。
	Create(ctx context.Context, in CreateInput) (Voice, error)
	// List answers all voices, updated_at DESC, id DESC.
	List(ctx context.Context) ([]Voice, error)
	// Get returns one voice or ErrVoiceNotFound.
	Get(ctx context.Context, id int64) (Voice, error)
	// Update applies the non-nil fields of u; ErrVoiceNotFound when there
	// is no row.
	Update(ctx context.Context, id int64, u *VoiceUpdate) (Voice, error)
	// Delete removes the ledger row; ErrVoiceInUse when a character still
	// binds it(解绑后才可删,不级联解绑).
	Delete(ctx context.Context, id int64) error
	// BoundCharacters counts character rows whose voice_id points here —
	// the DELETE handler's pre-check so the upstream delete never happens
	// for a locally refused row.
	BoundCharacters(ctx context.Context, id int64) (int, error)
	// EnsureSchema creates the voices table when missing (MySQL 与 SQLite
	// 双方言,voice_id 唯一索引:MySQL 表内 UNIQUE KEY,SQLite 独立
	// CREATE UNIQUE INDEX —— 名字两边一致),幂等.
	EnsureSchema(ctx context.Context) error
}
