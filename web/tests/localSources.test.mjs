import assert from 'node:assert/strict'
import { test } from 'node:test'
import { readFileSync } from 'node:fs'
import { localMemberRows, localSourceFilterOptions, canSaveLocalDraft, localPendingMessage, localMemberLabel, localReplacementDiff, localSaveIntent } from '../src/lib/localSourceDraft.ts'

test('共享订阅成员仍提供本地来源筛选入口', () => {
 const options = localSourceFilterOptions({ subscription: 1, local_source: 1 })
 assert.equal(options.find(o => o.value === 'local_source')?.label, '本地源 1')
 const page = readFileSync(new URL('../src/pages/NodeOverviewPage.tsx', import.meta.url), 'utf8')
 assert.match(page, /localSourceFilterOptions\(safeRecord\(data\?\.source_stats\)\)/)
})
test('完整成员健康映射保留可用失败未测和未应用且不泄漏URI', () => {
 const source = { nodes: Array.from({ length: 5 }, (_, i) => ({ uri: `http://secret@node.test:${i + 1}` })) }
 const rows = localMemberRows(source, ['available', 'unavailable', 'untested', 'unapplied', 'not_running'].map(status => ({ status })))
 assert.equal(rows.length, 5)
 assert.deepEqual(rows.map(r => r.health), ['可用', '不可用', '未检测', '未应用', '未运行'])
 assert.ok(!JSON.stringify(rows).includes('secret'))
 assert.equal(localMemberRows(source, [])[0].health, '状态待刷新')
 const component = readFileSync(new URL('../src/components/nodes/LocalSourcesSection.tsx', import.meta.url), 'utf8')
 assert.match(component, /localMemberRows\(/)
 assert.match(component, /probeNode\(/)
 assert.match(component, /shared_count/)
 assert.match(component, /其他来源仍提供时关闭本源不停止节点/)
 assert.match(component, /previewLocalSource\(text, editor.id, editor.revision\)/)
})

test('文本修改后预览失效，原样且格式合格才能保存', () => {
 assert.equal(canSaveLocalDraft('one', 'one', true), true)
 assert.equal(canSaveLocalDraft('two', 'one', true), false)
 assert.equal(canSaveLocalDraft('one', 'one', false), false)
 assert.equal(canSaveLocalDraft('', '', true), false)
})
test('替换显示新增移除保留，重命名无需重传成员', () => {
 const before = 'http://node.test:1#A\nhttp://node.test:2#B'
 const after = 'http://node.test:2#B\nhttp://node.test:3#C'
 assert.deepEqual(localReplacementDiff(before, after), { added: 1, removed: 1, retained: 1 })
 assert.deepEqual(localSaveIntent(before, before, null, false), { allowed: true, confirmReplacement: false, content: undefined })
 assert.equal(localSaveIntent(before, after, before, true).allowed, false)
 assert.deepEqual(localSaveIntent(before, after, after, true), { allowed: true, confirmReplacement: true, content: after })
})

test('组件接入差异预览和独立替换确认，启停不重传成员', () => {
 const component = readFileSync(new URL('../src/components/nodes/LocalSourcesSection.tsx', import.meta.url), 'utf8')
 assert.match(component, /localSaveIntent\(editor.original, editor.content, previewedText, !!preview\)/)
 assert.match(component, /localReplacementDiff\(editor.original, editor.content\)/)
 assert.match(component, /if \(intent.confirmReplacement\)[\s\S]*Modal.confirm\([\s\S]*确认替换本地源成员/)
 assert.match(component, /onSubmit=\{e => \{ e.preventDefault\(\); saveEditor\(\)/)
 assert.match(component, /name: row.name, enabled \}/)
})

test('删除最后一组仍显示服务端待应用状态', () => {
 assert.match(localPendingMessage({ pending: true, revision: 3, active_revision: 2, sources: [] }), /3/)
 assert.equal(localPendingMessage({ pending: false, revision: 3, active_revision: 3, sources: [] }), '')
})
test('成员清单无健康过滤，显示安全名称而不是连接凭据', () => {
 assert.equal(localMemberLabel('http://secret@node.test:8080#Berlin', 0), 'Berlin')
 assert.equal(localMemberLabel('vmess://secret', 1), '成员 2')
 assert.equal(localMemberLabel('http://secret@node.test#%ZZ', 0), '成员 1')
})
