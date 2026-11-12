<script setup lang="ts">
import { ref } from 'vue'

// 登录/初始化共用的 triptych 品牌外壳(86 号票,deal.mjs --type software):
// 左 headline 列(字标 + 一句 claim)/ 中作品拼贴(两组件 2.8s 交叉淡化轮换,
// 展示画布自己的节点卡缩微景)/ 右表单列。拼贴轮换是页面唯一常动的东西,
// 带暂停盘;reduced-motion 下整组静止只留第一组。
const collagePaused = ref(false)
</script>

<template>
  <main class="auth-shell">
    <section
      class="hero-head"
      aria-labelledby="auth-brand"
    >
      <h1
        id="auth-brand"
        class="brand"
      >
        <span class="mask"><span class="line brand-latin">InfiniteChance</span></span>
        <span class="mask"><span class="line brand-zh">无限画布</span></span>
      </h1>
      <p class="tagline">
        提示词 · 图片 · 视频 · 声音
      </p>
    </section>

    <!-- 作品拼贴:两套「画布现场」缩微景轮换(纯 CSS,件内零动画)。
       装饰件各自 aria-hidden(容器不能整体隐藏:暂停盘是可达控件)。 -->
    <section
      class="collage"
      :class="{ 'is-paused': collagePaused }"
    >
      <div
        class="set set-a"
        aria-hidden="true"
      >
        <div class="pc pc-node pc-image">
          <span class="pc-thumb thumb-image" />
          <span class="bar bright w70" />
          <span class="bar w45" />
        </div>
        <span class="pc pc-pill">seedance-2.0 · 1080p</span>
        <div class="pc pc-composer">
          <span class="bar bright w60" />
          <span class="send-dot" />
        </div>
        <div class="pc pc-node pc-video">
          <span class="pc-thumb thumb-video">
            <span class="play-glyph" />
          </span>
          <span class="bar bright w55" />
          <span class="dur-chip">00:08</span>
        </div>
      </div>

      <div
        class="set set-b"
        aria-hidden="true"
      >
        <div class="pc pc-strip">
          <span class="mini-frame" />
          <span class="mini-frame" />
          <span class="mini-frame" />
        </div>
        <div class="pc pc-node pc-script">
          <span class="bar bright w50" />
          <span class="bar w90" />
          <span class="bar w80" />
          <span class="bar w60" />
        </div>
        <div class="pc pc-group">
          <span class="bar bright w40" />
          <span class="mini-node" />
          <span class="mini-node" />
        </div>
        <div class="pc pc-node pc-character">
          <span class="avatar" />
          <span class="char-meta">
            <span class="bar bright w60" />
            <span class="chips">
              <span class="chip" />
              <span class="chip" />
            </span>
          </span>
        </div>
      </div>

      <button
        class="loop-toggle"
        type="button"
        :aria-label="collagePaused ? '播放动画' : '暂停动画'"
        @click="collagePaused = !collagePaused"
      >
        <span
          v-if="!collagePaused"
          class="tg tg-pause"
        />
        <span
          v-else
          class="tg tg-play"
        />
      </button>
    </section>

    <section class="auth-panel">
      <slot />
    </section>
  </main>
</template>

<style scoped>
.auth-shell {
  position: relative;
  min-height: 100vh;
  display: grid;
  grid-template-columns: minmax(260px, 380px) minmax(0, 1.5fr) minmax(340px, 400px);
  gap: clamp(32px, 4.5vw, 72px);
  align-content: center;
  max-width: 1400px;
  margin: 0 auto;
  padding: clamp(24px, 4vw, 56px);
}

/* bg-halo(86 号票 deal):标后光晕,accent 色相三层径向,上下渐隐入页底。 */
.auth-shell::before {
  content: '';
  position: absolute;
  inset: 0;
  z-index: 0;
  pointer-events: none;
  background:
    radial-gradient(34% 40% at 50% 34%, rgba(var(--accent-rgb), 0.1), transparent 70%),
    radial-gradient(70% 76% at 50% 34%, rgba(var(--accent-rgb), 0.05), transparent 75%),
    radial-gradient(30% 34% at 68% 52%, rgba(var(--indigo-soft-rgb), 0.08), transparent 72%);
  -webkit-mask-image: linear-gradient(to bottom, transparent, #000 12%, #000 88%, transparent);
  mask-image: linear-gradient(to bottom, transparent, #000 12%, #000 88%, transparent);
}

.hero-head,
.collage,
.auth-panel {
  position: relative;
  z-index: 1;
}

/* ---- 左列:headline(triptych 文法:屏高内垂直居中,字标下一条 16px 线) */
.hero-head {
  align-self: center;
}

.brand {
  margin: 0;
  display: grid;
  gap: 4px;
}

.brand-latin {
  font-family: var(--font-display);
  /* 词宽约 0.5em/字 × 14 字:1440 档 3.3vw ≈ 47px → 约 330px,须收在列宽内
     (mask overflow hidden 会水平裁字,字标不许换行也不许被裁)。 */
  font-size: clamp(40px, 3.3vw, 52px);
  font-weight: 500;
  font-stretch: 96%;
  letter-spacing: -0.036em;
  line-height: 1.04;
  white-space: nowrap;
  color: var(--text-bright);
}

.brand-zh {
  font-size: clamp(18px, 1.5vw, 22px);
  font-weight: 600;
  letter-spacing: 0.42em;
  color: var(--text-secondary);
}

.tagline {
  margin: 16px 0 0;
  color: var(--text-muted);
  font-size: var(--fs-body);
  letter-spacing: 0.08em;
}

/* ---- 中列:作品拼贴(628×716 文法,每组 3–5 件、件内零动画、
   暂停盘 32px 常驻右下) */
.collage {
  align-self: center;
  position: relative;
  width: 100%;
  max-width: calc(min(74vh, 716px) * 0.877);
  aspect-ratio: 628 / 716;
}

.set {
  position: absolute;
  inset: 0;
}

.pc {
  position: absolute;
  background: rgba(var(--surface-card-rgb), 0.92);
  border: 1px solid var(--line-control);
  border-radius: var(--r-xl);
  box-shadow: var(--glass-highlight);
}

.pc-node {
  padding: 8px;
  display: grid;
  gap: 6px;
}

/* 节点缩微件的左右端口圆点(与画布 .port 同一族,缩小到 8px) */
.pc-node::before,
.pc-node::after {
  content: '';
  position: absolute;
  top: 50%;
  width: 8px;
  height: 8px;
  border-radius: var(--r-pill);
  background: rgba(var(--surface-panel-rgb), 1);
  border: 1px solid var(--line-action);
}

.pc-node::before {
  left: -5px;
}

.pc-node::after {
  right: -5px;
}

.pc-image {
  left: 3%;
  top: 5%;
  width: 46%;
  border-color: rgba(var(--success-rgb), 0.4);
}

.pc-video {
  right: 4%;
  bottom: 4%;
  width: 50%;
  border-color: rgba(var(--amber-rgb), 0.42);
}

.pc-script {
  right: 4%;
  top: 6%;
  width: 42%;
  border-color: rgba(var(--gold-rgb), 0.42);
}

.pc-character {
  right: 4%;
  bottom: 4%;
  width: 54%;
  border-color: rgba(var(--teal-rgb), 0.42);
  display: flex;
  align-items: center;
  gap: 10px;
}

.pc-composer {
  left: 3%;
  bottom: 6%;
  width: 50%;
  border-color: rgba(var(--accent-rgb), 0.42);
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 10px;
}

.pc-pill {
  right: 8%;
  top: 40%;
  padding: 5px 12px;
  border-radius: var(--r-pill);
  background: rgba(var(--surface-panel-rgb), 0.9);
  font-family: var(--font-mono);
  font-size: 10px;
  letter-spacing: 0.02em;
  color: var(--text-muted);
}

.pc-strip {
  left: 3%;
  top: 5%;
  width: 52%;
  border-color: rgba(var(--orange-rgb), 0.42);
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 6px;
  padding: 8px;
}

.pc-group {
  left: 3%;
  bottom: 6%;
  width: 48%;
  border-style: dashed;
  display: grid;
  gap: 6px;
  padding: 9px 10px;
}

/* 缩微件内部的小件 */
.pc-thumb {
  display: block;
  aspect-ratio: 16 / 10;
  border-radius: var(--r-md);
}

.thumb-image {
  background: linear-gradient(135deg, rgba(var(--success-rgb), 0.35), rgba(var(--sky-rgb), 0.28));
}

.thumb-video {
  position: relative;
  background: linear-gradient(135deg, rgba(var(--amber-rgb), 0.3), rgba(var(--orange-rgb), 0.24));
}

.play-glyph {
  position: absolute;
  left: 50%;
  top: 50%;
  width: 14px;
  height: 14px;
  transform: translate(-50%, -50%);
  background: rgba(var(--surface-panel-rgb), 0.9);
  clip-path: polygon(18% 0, 100% 50%, 18% 100%);
}

.dur-chip {
  position: absolute;
  right: 14px;
  bottom: 34px;
  padding: 2px 6px;
  border-radius: var(--r-sm);
  background: rgba(var(--surface-panel-rgb), 0.9);
  font-family: var(--font-mono);
  font-size: 9px;
  color: var(--text-muted);
}

.send-dot {
  flex: none;
  width: 26px;
  height: 26px;
  border-radius: var(--r-pill);
  background-color: var(--accent);
  /* 发送箭头:panel 底色小三角 */
  background-image: linear-gradient(45deg, transparent 50%, var(--accent-ink) 50%);
  background-size: 8px 8px;
  background-position: right 5px center;
  background-repeat: no-repeat;
}

.avatar {
  flex: none;
  width: 34px;
  height: 34px;
  border-radius: var(--r-pill);
  background: linear-gradient(135deg, rgba(var(--teal-rgb), 0.4), rgba(var(--teal-light-rgb), 0.3));
}

.char-meta {
  flex: 1;
  display: grid;
  gap: 6px;
}

.chips {
  display: flex;
  gap: 4px;
}

.chip {
  width: 26px;
  height: 10px;
  border-radius: var(--r-pill);
  background: var(--wash-fill);
}

.mini-frame {
  display: block;
  aspect-ratio: 16 / 10;
  border-radius: var(--r-sm);
  background: var(--wash-soft);
  border: 1px solid rgba(var(--orange-rgb), 0.3);
}

.mini-node {
  height: 18px;
  border-radius: var(--r-sm);
  background: var(--wash-soft);
  border: 1px solid var(--line-section);
}

.bar {
  display: block;
  height: 6px;
  border-radius: 3px;
  background: var(--wash-fill);
}

.bar.bright {
  background: var(--wash-active);
}

.w40 { width: 40%; }
.w45 { width: 45%; }
.w50 { width: 50%; }
.w55 { width: 55%; }
.w60 { width: 60%; }
.w70 { width: 70%; }
.w80 { width: 80%; }
.w90 { width: 90%; }

/* 暂停盘(32px 实心盘,右下 12px,skill 动效预算:常动件必配暂停) */
.loop-toggle {
  position: absolute;
  right: 12px;
  bottom: 12px;
  z-index: 2;
  width: 32px;
  height: 32px;
  display: grid;
  place-items: center;
  border: 1px solid var(--line-control);
  border-radius: var(--r-pill);
  background: rgba(var(--surface-panel-rgb), 0.95);
  color: var(--text-secondary);
  cursor: pointer;
  box-shadow: var(--shadow-control);
  transition:
    border-color var(--dur-fast) var(--ease),
    color var(--dur-fast) var(--ease);
}

.loop-toggle:hover {
  border-color: rgba(var(--accent-rgb), 0.6);
  color: var(--text-primary);
}

.tg {
  display: block;
  width: 10px;
  height: 10px;
}

.tg-pause {
  background: linear-gradient(
    90deg,
    currentColor 0 38%,
    transparent 38% 62%,
    currentColor 62% 100%
  );
}

.tg-play {
  background: currentColor;
  clip-path: polygon(16% 0, 100% 50%, 16% 100%);
}

/* ---- 右列:表单面板 */
.auth-panel {
  align-self: center;
  background: var(--wash-subtle);
  border: 1px solid var(--line-section);
  border-radius: var(--r-xl);
  padding: 32px 28px;
}

/* ---- 窄屏(<1120px):三列堆叠,内容列居中(页面轴) */
@media (max-width: 1120px) {
  .auth-shell {
    grid-template-columns: minmax(0, 520px);
    justify-content: center;
    align-content: start;
    gap: 32px;
    padding-top: 56px;
  }

  .hero-head {
    justify-self: center;
    text-align: center;
  }

  .collage {
    max-width: min(520px, 88vw);
    /* 窄屏高度上限:让表单列在首屏折叠线前露出(triptych 390 文法:
       headline → collage → button 同屏收场),拼贴盒变矮不减件数。 */
    max-height: 320px;
  }

  .auth-panel {
    max-width: 460px;
    width: 100%;
    justify-self: center;
  }
}

/* ---- 入场(skill hero entrance):字标逐行出罩,随后元素 12px fade-up,
   约 1.1s 收场;once,transform/opacity only ---- */
@media (prefers-reduced-motion: no-preference) {
  .mask {
    display: block;
    overflow: hidden;
  }

  .mask .line {
    display: inline-block;
    transform: translateY(115%);
    animation: auth-line-rise 700ms var(--ease-enter) forwards;
  }

  .mask:nth-of-type(2) .line {
    animation-delay: 80ms;
  }

  .tagline {
    opacity: 0;
    animation: auth-fade-up 520ms var(--ease-enter) 240ms forwards;
  }

  .collage {
    opacity: 0;
    animation: auth-collage-settle 900ms var(--ease-enter) 320ms forwards;
  }

  /* 两组件轮换:5.6s 周期,交叉淡化窗 0.7s(37.5%–50% 与 87.5%–100%);
   暂停盘冻结 animation-play-state。B 组负 delay 错开半周期——必须写在
   animation 简写之后,否则简写会把它重置回 0(两组同相 = 空窗)。 */
  .set {
    animation: auth-set-cycle 5.6s linear infinite;
  }

  .set-b {
    animation-delay: -2.8s;
  }

  .collage.is-paused .set {
    animation-play-state: paused;
  }

  .auth-panel {
    opacity: 0;
    animation: auth-fade-up 520ms var(--ease-enter) 400ms forwards;
  }
}

@media (prefers-reduced-motion: reduce) {
  /* 只留第一组,轮换静止 */
  .set-b {
    display: none;
  }
}

@keyframes auth-line-rise {
  to {
    transform: translateY(0);
  }
}

@keyframes auth-fade-up {
  from {
    opacity: 0;
    transform: translateY(12px);
  }

  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@keyframes auth-collage-settle {
  from {
    opacity: 0;
    transform: scale(0.98);
  }

  to {
    opacity: 1;
    transform: scale(1);
  }
}

@keyframes auth-set-cycle {
  0%,
  37.5% {
    opacity: 1;
  }

  50%,
  87.5% {
    opacity: 0;
  }

  100% {
    opacity: 1;
  }
}
</style>
