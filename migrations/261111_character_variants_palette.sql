-- 84 号票(色指定增量):character_variants 加变体配色列 palette——自由
-- 文本 HEX 行(如「发色:#1A2E;肤色:#FFE0D0;…」),handler 层限 500
-- runes(照 69 号票 MaxIPStyleRunes 先例,超限拒绝)。TEXT NOT NULL 无
-- 显式默认(MySQL TEXT 列不支持字面 DEFAULT),加列时存量行按隐式默认
-- '' 填充,读回空串 = 无配色、生成注入面整段跳过。
--
-- 本文件是留底,从不执行(ADR 0008):运行时加列由
-- internal/character 的 EnsureSchema 幂等自愈(与 SQLite 侧同形)。
ALTER TABLE character_variants ADD COLUMN palette TEXT NOT NULL;
