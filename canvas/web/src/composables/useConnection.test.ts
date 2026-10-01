import { describe, expect, it } from 'vitest'

import { useConnection } from './useConnection'

function setup(nodes: { id: string; type: string }[]) {
  const committed: { source: string; target: string }[] = []
  const connection = useConnection({
    nodes: () => nodes,
    onCommit: (edge) => committed.push(edge),
  })
  return { committed, connection }
}

describe('useConnection', () => {
  it('enters connection mode on start and exits on cancel', () => {
    const { connection } = setup([{ id: 'a', type: 'agent' }])
    expect(connection.active.value).toBe(false)
    connection.start('a', 'right')
    expect(connection.active.value).toBe(true)
    expect(connection.pending.value).toEqual({ nodeId: 'a', side: 'right' })
    connection.cancel()
    expect(connection.active.value).toBe(false)
    expect(connection.pending.value).toBeNull()
  })

  it('cancels when the same plus button is clicked again', () => {
    const { connection } = setup([{ id: 'a', type: 'agent' }])
    connection.start('a', 'left')
    connection.start('a', 'left')
    expect(connection.active.value).toBe(false)
  })

  it('switches origin when another plus button is clicked mid-connection', () => {
    const { connection } = setup([{ id: 'a', type: 'agent' }, { id: 'b', type: 'image' }])
    connection.start('a', 'right')
    connection.start('b', 'left')
    expect(connection.pending.value).toEqual({ nodeId: 'b', side: 'left' })
  })

  it('commits right-side connections with the origin as source', () => {
    const { committed, connection } = setup([
      { id: 'a', type: 'agent' },
      { id: 'img', type: 'image' },
    ])
    connection.start('a', 'right')
    connection.clickNode('img')
    expect(committed).toEqual([{ source: 'a', target: 'img' }])
    expect(connection.active.value).toBe(false)
  })

  it('commits left-side connections with the clicked node as source', () => {
    const { committed, connection } = setup([
      { id: 'vid', type: 'video' },
      { id: 'a', type: 'agent' },
    ])
    // agent 的左 + 接上游:点视频节点完成 video→agent(反推来源方向)。
    connection.start('a', 'left')
    connection.clickNode('vid')
    expect(committed).toEqual([{ source: 'vid', target: 'a' }])
  })

  it('cancels without committing on invalid or self targets', () => {
    const { committed, connection } = setup([
      { id: 'a', type: 'agent' },
      { id: 'b', type: 'analysis' },
    ])
    connection.start('a', 'right')
    connection.clickNode('b')
    expect(committed).toEqual([])
    expect(connection.active.value).toBe(false)

    connection.start('a', 'right')
    connection.clickNode('a')
    expect(committed).toEqual([])
    expect(connection.active.value).toBe(false)
  })

  it('tracks the pointer only while pending and clears it on cancel', () => {
    const { connection } = setup([{ id: 'a', type: 'agent' }])
    connection.moveTo({ x: 1, y: 2 })
    expect(connection.pointer.value).toBeNull()

    connection.start('a', 'right')
    connection.moveTo({ x: 1, y: 2 })
    expect(connection.pointer.value).toEqual({ x: 1, y: 2 })
    connection.cancel()
    expect(connection.pointer.value).toBeNull()
  })

  it('classifies nodes as origin, valid and dimmed during connection', () => {
    const { connection } = setup([
      { id: 'a', type: 'agent' },
      { id: 'img', type: 'image' },
      { id: 'an', type: 'analysis' },
    ])
    expect(connection.stateOf('img')).toBeUndefined()

    connection.start('a', 'right')
    expect(connection.stateOf('a')).toBe('origin')
    expect(connection.stateOf('img')).toBe('valid')
    expect(connection.stateOf('an')).toBe('dimmed')

    connection.cancel()
    expect(connection.stateOf('img')).toBeUndefined()
  })
})
