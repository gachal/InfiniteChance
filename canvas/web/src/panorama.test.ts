// 全景域纯函数的测试(81 号票):2:1 判定与偏离度、截图比例档与尺寸表、
// 任务对节点自身的回填语义(原地替换)。测试自造轻量对象,不依赖 api 包
// (composer.test.ts 同款纪律)。
import { describe, expect, it } from 'vitest'

import {
  PANO_CAPTURE_DEFAULT_RATIO,
  PANO_CAPTURE_LONG_EDGE,
  PANO_CAPTURE_RATIOS,
  PANO_DEFAULT_MODEL,
  PANO_SCENE_TYPE,
  captureSize,
  isPanoRatio,
  panoRatioDeviation,
  panoramaSyncPatch,
  uploadPanoramaCapture,
} from './panorama'

describe('isPanoRatio / panoRatioDeviation(2:1 ±2%)', () => {
  it('打样产物 7680×3840(8K)精确命中 2:1', () => {
    expect(isPanoRatio(7680, 3840)).toBe(true)
    expect(panoRatioDeviation(7680, 3840)).toBe(0)
  })

  it('±2% 边界内接受:2.04:1(偏离 2.0%)算全景,2.05:1(偏离 2.5%)不算', () => {
    expect(isPanoRatio(2040, 1000)).toBe(true)
    expect(panoRatioDeviation(2040, 1000)).toBeCloseTo(2, 10)
    expect(isPanoRatio(1960, 1000)).toBe(true)
    expect(isPanoRatio(2050, 1000)).toBe(false)
    expect(isPanoRatio(1950, 1000)).toBe(false)
  })

  it('普通画幅一律拒绝:16:9 / 4:3 / 1:1 / 竖图', () => {
    expect(isPanoRatio(1920, 1080)).toBe(false)
    expect(isPanoRatio(1440, 1080)).toBe(false)
    expect(isPanoRatio(1024, 1024)).toBe(false)
    expect(isPanoRatio(1088, 1920)).toBe(false)
  })

  it('零/负尺寸拒绝(图片未加载时 naturalWidth 为 0)', () => {
    expect(isPanoRatio(0, 0)).toBe(false)
    expect(isPanoRatio(100, 0)).toBe(false)
    expect(isPanoRatio(-2, 1)).toBe(false)
    expect(panoRatioDeviation(0, 0)).toBe(Number.POSITIVE_INFINITY)
  })
})

describe('PANO_CAPTURE_RATIOS / captureSize(查看层截图,长边固定 1920)', () => {
  it('恰好五档:1:1 / 16:9 / 9:16 / 4:3 / 3:4,缺省 16:9', () => {
    expect(PANO_CAPTURE_RATIOS.map((r) => r.value)).toEqual(['1:1', '16:9', '9:16', '4:3', '3:4'])
    expect(PANO_CAPTURE_DEFAULT_RATIO).toBe('16:9')
  })

  it('长边恒 1920:横档宽 1920、竖档高 1920、方档两边 1920', () => {
    expect(PANO_CAPTURE_LONG_EDGE).toBe(1920)
    expect(captureSize('16:9')).toEqual({ width: 1920, height: 1080 })
    expect(captureSize('9:16')).toEqual({ width: 1080, height: 1920 })
    expect(captureSize('1:1')).toEqual({ width: 1920, height: 1920 })
    expect(captureSize('4:3')).toEqual({ width: 1920, height: 1440 })
    expect(captureSize('3:4')).toEqual({ width: 1440, height: 1920 })
  })

  it('未知档位回落缺省 16:9(下拉只出五档,防御坏数据)', () => {
    expect(captureSize('21:9')).toEqual({ width: 1920, height: 1080 })
    expect(captureSize('')).toEqual({ width: 1920, height: 1080 })
  })
})

describe('PANO_SCENE_TYPE / PANO_DEFAULT_MODEL(通路常量)', () => {
  it('场景类型与缺省公开名按定案钉死', () => {
    expect(PANO_SCENE_TYPE).toBe('3d_panorama')
    expect(PANO_DEFAULT_MODEL).toBe('hunyuan-world-panorama')
  })
})

describe('uploadPanoramaCapture(查看层截图入库共享流,评审修复轮提取)', () => {
  /** 宿主上传函数的替身:记下收到的 (file, kind, ipId),返回素材行形状。 */
  function fakeUpload() {
    const calls: Array<{ name: string; type: string; kind: string; ipId?: number }> = []
    const upload = async (file: File, kind: 'image', ipId?: number) => {
      calls.push({ name: file.name, type: file.type, kind, ipId })
      return { id: 1 }
    }
    return { calls, upload }
  }

  it('blob → panorama-<ts>.jpg(kind=image),IP 归属透传给宿主上传函数', async () => {
    const { calls, upload } = fakeUpload()
    await uploadPanoramaCapture(upload, new Blob(['x'], { type: 'image/jpeg' }), 7)
    expect(calls).toHaveLength(1)
    expect(calls[0]!.name).toMatch(/^panorama-\d+\.jpg$/)
    expect(calls[0]!.type).toBe('image/jpeg')
    expect(calls[0]!.kind).toBe('image')
    expect(calls[0]!.ipId).toBe(7)
  })

  it('未声明 IP 的宿主透传 undefined(全局口径,不虚构 0)', async () => {
    const { calls, upload } = fakeUpload()
    await uploadPanoramaCapture(upload, new Blob(['x']))
    expect(calls[0]!.ipId).toBeUndefined()
  })

  it('宿主上传函数抛错时原样上抛(失败路径留在宿主,这里只管上传)', async () => {
    await expect(
      uploadPanoramaCapture(
        async () => {
          throw new Error('boom')
        },
        new Blob(['x']),
      ),
    ).rejects.toThrow('boom')
  })
})

describe('panoramaSyncPatch(任务回填节点自身,重新生成 = 原地替换)', () => {
  const succeeded = {
    kind: 'image',
    status: 'succeeded',
    prompt: '',
    model: 'hunyuan-world-panorama',
    asset_id: 42,
    image_url: 'https://vendor.example/tmp.jpg',
    video_url: '',
  }

  it('成功且带素材行:回填内容寻址地址 + asset_id + model', () => {
    const patch = panoramaSyncPatch(succeeded, { ref_asset_id: 7 })
    expect(patch).toEqual({
      url: '/api/assets/42/content',
      asset_id: 42,
      model: 'hunyuan-world-panorama',
    })
  })

  it('原地替换:节点已带旧产物时 url/asset_id/model 照样覆盖(旧产物留素材库)', () => {
    const patch = panoramaSyncPatch(succeeded, {
      ref_asset_id: 7,
      url: '/api/assets/11/content',
      asset_id: 11,
      model: 'og-image-2.5',
    })
    expect(patch).toEqual({
      url: '/api/assets/42/content',
      asset_id: 42,
      model: 'hunyuan-world-panorama',
    })
  })

  it('任务行 model 为空不物化 model 键(不虚构模型事实)', () => {
    const patch = panoramaSyncPatch({ ...succeeded, model: '' }, { ref_asset_id: 7 })
    expect(patch).toEqual({ url: '/api/assets/42/content', asset_id: 42 })
    expect('model' in patch!).toBe(false)
  })

  it('与已落节点完全一致时返回 null(轮询重复同步不落盘)', () => {
    const patch = panoramaSyncPatch(succeeded, {
      ref_asset_id: 7,
      url: '/api/assets/42/content',
      asset_id: 42,
      model: 'hunyuan-world-panorama',
    })
    expect(patch).toBeNull()
  })

  it('非成功态 / 无素材行(且无厂商地址回落资格)返回 null', () => {
    expect(panoramaSyncPatch({ ...succeeded, status: 'running' }, { ref_asset_id: 7 })).toBeNull()
    expect(panoramaSyncPatch({ ...succeeded, status: 'failed' }, { ref_asset_id: 7 })).toBeNull()
    const noAsset = panoramaSyncPatch(
      { ...succeeded, asset_id: 0, image_url: '' },
      { ref_asset_id: 7 },
    )
    expect(noAsset).toBeNull()
  })

  it('ref_asset_id 是创作事实,永不出现在补丁里', () => {
    const patch = panoramaSyncPatch(succeeded, { ref_asset_id: 7 })
    expect(patch && 'ref_asset_id' in patch).toBe(false)
  })
})
