-- 84 号票(色指定增量):character_variants 加变体配色列 palette——自由
-- 文本 HEX 行(如「发色:#1A2E;肤色:#FFE0D0;…」),handler 层限 500
-- runes(照 69 号票 MaxIPStyleRunes 先例,超限拒绝)。SQLite 的 ADD COLUMN
-- 带 NOT NULL 约束时必须给常量默认值,存量行读回 '' = 无配色、生成注入面
-- 整段跳过(与 MySQL 侧隐式默认语义一致)。
--
-- 本文件是留底,从不执行(ADR 0008):运行时加列由
-- internal/character 的 EnsureSchema 幂等自愈(与 MySQL 侧同形)。
ALTER TABLE character_variants ADD COLUMN palette TEXT NOT NULL DEFAULT '';
