-- InfiniteChance 全库 schema 基线(MySQL)—— 82 号票留底,本文件冻结不改。
--
-- 生成方法(2026-11-11):临时空库按 desktop/runtime.go 装配顺序跑齐 17 个
-- store 的 EnsureSchema,再逐表 SHOW CREATE TABLE;仅归一表选项尾部
-- AUTO_INCREMENT=N 计数器,其余原样保留 —— 与运行时真形同源,拒绝手工翻译。
-- 表顺序 = 装配顺序:网关域 10 表在前、画布域 12 表在后,出处见各表注释。
--
-- 冻结声明:本文件落档后不再修改;未来结构 = 本基线 + migrations/ 下
-- yyMMdd_*.sql 增量链(增量与 .sqlite.sql 成对,命名纪律见 CONTEXT.md
-- 「迁移留底」词条)。三处历史非 DDL 迁移(apikey expires_at 改型、auth 去
-- AUTO_INCREMENT、ips 默认 IP 自愈)不回填本文件,历史演进看 git(ADR 0008)。
--
-- 文件从不执行:运行时机制仍是各 store 的 EnsureSchema 双方言自愈,无 runner、
-- 无记账表。internal/migrations 的形状对账测试把「基线 + 增量链」真实执行后与
-- 全部 EnsureSchema 终形逐表比对表/列/索引名集合,持续证明基线与运行时同形。

-- admin_accounts(internal/auth)
CREATE TABLE `admin_accounts` (
  `id` bigint unsigned NOT NULL,
  `username` varchar(64) NOT NULL,
  `password_hash` varchar(255) NOT NULL,
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uniq_admin_username` (`username`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- channels(internal/channel)
CREATE TABLE `channels` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `name` varchar(64) NOT NULL,
  `type` varchar(32) NOT NULL,
  `base_url` varchar(512) NOT NULL,
  `api_key` text NOT NULL,
  `model_map` json NOT NULL,
  `capabilities` json DEFAULT NULL,
  `config` json DEFAULT NULL,
  `priority` int NOT NULL DEFAULT '0',
  `weight` int NOT NULL DEFAULT '0',
  `enabled` tinyint(1) NOT NULL DEFAULT '1',
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- api_keys(internal/apikey)
CREATE TABLE `api_keys` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `name` varchar(64) NOT NULL,
  `prefix` varchar(16) NOT NULL,
  `key_hash` char(64) NOT NULL,
  `quota_micros` bigint NOT NULL DEFAULT '0',
  `expires_at` datetime(6) DEFAULT NULL,
  `revoked_at` timestamp(6) NULL DEFAULT NULL,
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uniq_api_key_hash` (`key_hash`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- api_key_quota_log(internal/apikey)
CREATE TABLE `api_key_quota_log` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `key_id` bigint unsigned NOT NULL,
  `delta_micros` bigint NOT NULL,
  `balance_micros` bigint NOT NULL,
  `reason` varchar(64) NOT NULL,
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  KEY `idx_api_key_quota_log_key` (`key_id`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- model_prices(internal/pricing)
CREATE TABLE `model_prices` (
  `public_model` varchar(200) NOT NULL,
  `unit` varchar(16) NOT NULL,
  `config` json NOT NULL,
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`public_model`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- usage_logs(internal/usage)
CREATE TABLE `usage_logs` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `key_id` bigint unsigned NOT NULL,
  `channel_id` bigint unsigned NOT NULL,
  `channel_name` varchar(64) NOT NULL,
  `public_model` varchar(200) NOT NULL,
  `upstream_model` varchar(200) NOT NULL,
  `unit` varchar(16) NOT NULL,
  `prompt_tokens` bigint NOT NULL DEFAULT '0',
  `completion_tokens` bigint NOT NULL DEFAULT '0',
  `duration_ms` bigint NOT NULL DEFAULT '0',
  `status` varchar(32) NOT NULL,
  `charge_micros` bigint NOT NULL DEFAULT '0',
  `price_snapshot` json DEFAULT NULL,
  `upstream_error` text,
  `source` varchar(255) DEFAULT NULL,
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  KEY `idx_usage_logs_key` (`key_id`,`id`),
  KEY `idx_usage_logs_channel` (`channel_id`,`id`),
  KEY `idx_usage_logs_model` (`public_model`,`id`),
  KEY `idx_usage_logs_created` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- video_tasks(internal/videotask)
CREATE TABLE `video_tasks` (
  `id` varchar(64) NOT NULL,
  `key_id` bigint unsigned NOT NULL,
  `channel_id` bigint unsigned NOT NULL,
  `channel_name` varchar(64) NOT NULL,
  `public_model` varchar(200) NOT NULL,
  `upstream_model` varchar(200) NOT NULL,
  `upstream_task_id` varchar(255) NOT NULL,
  `status` varchar(16) NOT NULL,
  `upstream_status` varchar(64) DEFAULT NULL,
  `size` varchar(64) NOT NULL DEFAULT '',
  `seconds` bigint NOT NULL DEFAULT '0',
  `video_url` text,
  `error` text,
  `reserved_micros` bigint NOT NULL DEFAULT '0',
  `charge_micros` bigint NOT NULL DEFAULT '0',
  `price_snapshot` json DEFAULT NULL,
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  KEY `idx_video_tasks_key` (`key_id`,`id`),
  KEY `idx_video_tasks_status` (`status`,`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- music_tasks(internal/musictask)
CREATE TABLE `music_tasks` (
  `id` varchar(64) NOT NULL,
  `key_id` bigint unsigned NOT NULL,
  `channel_id` bigint unsigned NOT NULL,
  `channel_name` varchar(64) NOT NULL,
  `public_model` varchar(200) NOT NULL,
  `upstream_model` varchar(200) NOT NULL,
  `upstream_task_id` varchar(255) NOT NULL,
  `status` varchar(16) NOT NULL,
  `upstream_status` varchar(64) DEFAULT NULL,
  `audio_url` text,
  `error` text,
  `reserved_micros` bigint NOT NULL DEFAULT '0',
  `charge_micros` bigint NOT NULL DEFAULT '0',
  `price_snapshot` json DEFAULT NULL,
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  KEY `idx_music_tasks_key` (`key_id`,`id`),
  KEY `idx_music_tasks_status` (`status`,`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- prompt_templates(internal/prompttemplate)
CREATE TABLE `prompt_templates` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `name` varchar(128) NOT NULL,
  `description` varchar(200) NOT NULL DEFAULT '',
  `template` mediumtext NOT NULL,
  `target` varchar(16) NOT NULL DEFAULT 'any',
  `enabled` tinyint(1) NOT NULL DEFAULT '1',
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- settings(internal/settings)
CREATE TABLE `settings` (
  `name` varchar(64) NOT NULL,
  `value` json NOT NULL,
  `updated_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- canvases(internal/canvas)
CREATE TABLE `canvases` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `name` varchar(128) NOT NULL,
  `graph` json NOT NULL,
  `version` bigint unsigned NOT NULL DEFAULT '1',
  `ip_id` bigint unsigned NOT NULL DEFAULT '0',
  `type` varchar(32) NOT NULL DEFAULT 'animation',
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- assets(internal/asset)
CREATE TABLE `assets` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `kind` varchar(16) NOT NULL,
  `canvas_id` bigint unsigned NOT NULL,
  `task_id` varchar(64) NOT NULL DEFAULT '',
  `model` varchar(200) NOT NULL DEFAULT '',
  `prompt` text,
  `url` mediumtext NOT NULL,
  `object_key` varchar(512) NOT NULL DEFAULT '',
  `content_type` varchar(100) NOT NULL DEFAULT '',
  `size_bytes` bigint NOT NULL DEFAULT '0',
  `content_hash` varchar(64) NOT NULL DEFAULT '',
  `ip_id` bigint unsigned NOT NULL DEFAULT '0',
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `last_used_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  KEY `idx_assets_canvas` (`canvas_id`,`id`),
  KEY `idx_assets_task` (`task_id`),
  KEY `idx_assets_kind` (`kind`,`id`),
  KEY `idx_assets_last_used` (`last_used_at`,`id`),
  KEY `idx_assets_object_key` (`object_key`),
  KEY `idx_assets_hash` (`content_hash`),
  KEY `idx_assets_ip` (`ip_id`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ips(internal/canvas(IPStore))
CREATE TABLE `ips` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `name` varchar(255) NOT NULL,
  `description` varchar(1024) NOT NULL DEFAULT '',
  `cover_asset_id` bigint unsigned NOT NULL DEFAULT '0',
  `style` text,
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- characters(internal/character)
CREATE TABLE `characters` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `ip_id` bigint unsigned NOT NULL,
  `name` varchar(255) NOT NULL,
  `alias` varchar(255) NOT NULL DEFAULT '',
  `description` varchar(1024) NOT NULL DEFAULT '',
  `detail` text NOT NULL,
  `voice_id` bigint unsigned DEFAULT NULL,
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  KEY `idx_characters_ip` (`ip_id`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- character_variants(internal/character)
CREATE TABLE `character_variants` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `character_id` bigint unsigned NOT NULL,
  `name` varchar(255) NOT NULL,
  `note` varchar(1024) NOT NULL DEFAULT '',
  `sort` int NOT NULL DEFAULT '0',
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  KEY `idx_character_variants_character` (`character_id`,`sort`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- character_images(internal/character)
CREATE TABLE `character_images` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `character_id` bigint unsigned NOT NULL,
  `variant_id` bigint unsigned NOT NULL,
  `asset_id` bigint unsigned NOT NULL,
  `label` varchar(32) NOT NULL DEFAULT '',
  `sort` int NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  KEY `idx_character_images_character` (`character_id`,`sort`,`id`),
  KEY `idx_character_images_variant` (`variant_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- scenes(internal/scene)
CREATE TABLE `scenes` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `ip_id` bigint unsigned NOT NULL,
  `name` varchar(255) NOT NULL,
  `alias` varchar(255) NOT NULL DEFAULT '',
  `description` varchar(1024) NOT NULL DEFAULT '',
  `detail` text NOT NULL,
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  KEY `idx_scenes_ip` (`ip_id`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- scene_images(internal/scene)
CREATE TABLE `scene_images` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `scene_id` bigint unsigned NOT NULL,
  `asset_id` bigint unsigned NOT NULL,
  `label` varchar(32) NOT NULL DEFAULT '',
  `sort` int NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  KEY `idx_scene_images_scene` (`scene_id`,`sort`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- props(internal/prop)
CREATE TABLE `props` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `ip_id` bigint unsigned NOT NULL,
  `name` varchar(255) NOT NULL,
  `alias` varchar(255) NOT NULL DEFAULT '',
  `description` varchar(1024) NOT NULL DEFAULT '',
  `detail` text NOT NULL,
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  KEY `idx_props_ip` (`ip_id`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- prop_images(internal/prop)
CREATE TABLE `prop_images` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `prop_id` bigint unsigned NOT NULL,
  `asset_id` bigint unsigned NOT NULL,
  `label` varchar(32) NOT NULL DEFAULT '',
  `sort` int NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  KEY `idx_prop_images_prop` (`prop_id`,`sort`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- voices(internal/voice)
CREATE TABLE `voices` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `voice_id` varchar(255) NOT NULL,
  `channel_id` bigint unsigned NOT NULL,
  `target_model` varchar(200) NOT NULL,
  `name` varchar(255) NOT NULL,
  `source_mode` varchar(16) NOT NULL,
  `source_prompt` text,
  `sample_asset_id` bigint unsigned DEFAULT NULL,
  `preview_asset_id` bigint unsigned DEFAULT NULL,
  `status` varchar(16) NOT NULL,
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_voices_voice_id` (`voice_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- canvas_tasks(internal/canvastask)
CREATE TABLE `canvas_tasks` (
  `id` varchar(64) NOT NULL,
  `canvas_id` bigint unsigned NOT NULL,
  `node_id` varchar(128) NOT NULL,
  `ip_id` bigint unsigned NOT NULL DEFAULT '0',
  `kind` varchar(16) NOT NULL,
  `prompt` text NOT NULL,
  `model` varchar(200) NOT NULL,
  `size` varchar(64) NOT NULL DEFAULT '',
  `seconds` bigint NOT NULL DEFAULT '0',
  `lyrics` mediumtext,
  `instrumental` tinyint NOT NULL DEFAULT '1',
  `audio_ref` mediumtext,
  `cover_strength` float DEFAULT NULL,
  `image_ref` mediumtext,
  `image_refs` mediumtext,
  `video_refs` mediumtext,
  `ratio` varchar(32) NOT NULL DEFAULT '',
  `background` varchar(32) NOT NULL DEFAULT '',
  `scene_type` varchar(32) NOT NULL DEFAULT '',
  `status` varchar(16) NOT NULL,
  `attempts` bigint NOT NULL DEFAULT '0',
  `error` text,
  `asset_id` bigint unsigned DEFAULT NULL,
  `image_url` mediumtext,
  `video_url` mediumtext,
  `remote_task_id` varchar(64) NOT NULL DEFAULT '',
  `voice_spec` mediumtext,
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  KEY `idx_canvas_tasks_canvas` (`canvas_id`,`created_at`),
  KEY `idx_canvas_tasks_status` (`status`,`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

