import assert from 'node:assert/strict'
import { test } from 'node:test'
import { buildSettingsSavePayload, isSettingsDraftDirty, isSubscriptionDraftDirty } from '../src/pages/settingsSavePayload.ts'

const alpha = 'https://example.test/alpha'
const beta = 'https://example.test/beta'
const gamma = 'https://example.test/gamma'

const settings = {
  subscriptions: [alpha, beta],
  subscription_names: [' Alpha 云 ', 'Beta 云'],
}

test('节点来源优先显示订阅名，缺少名称时保留各页面的原有来源', async () => {
  const { nodeSourceLabel } = await import('../src/lib/subscriptionAttribution.ts')
  assert.equal(nodeSourceLabel({ subscription_name: ' Alpha 云 ' }, '订阅'), 'Alpha 云')
  assert.equal(nodeSourceLabel({}, '订阅/节点文件'), '订阅/节点文件')
  assert.equal(nodeSourceLabel({ subscription_name: '  ' }, '文件'), '文件')
})

test('订阅设置和节点来源统一显示魔戒简称', async () => {
  const { nodeSourceLabel, subscriptionDraftLabels } = await import('../src/lib/subscriptionAttribution.ts')
  assert.equal(nodeSourceLabel({ subscription_name: ' 魔戒.net ' }, '订阅'), '魔戒')
  assert.deepEqual(subscriptionDraftLabels([alpha, beta], { subscriptions: [alpha, beta], subscription_names: ['魔戒.net', 'Beta.net'] }), ['魔戒', 'Beta.net'])
})

test('节点配置可搜索订阅名，并保留来源筛选和原有搜索字段', async () => {
  const { filterConfigNodes } = await import('../src/lib/subscriptionAttribution.ts')
  const subscribed = { name: '大阪节点', uri: 'example.test', source: 'subscription', port: 1234, subscription_name: 'Alpha 云' }
  const inline = { name: '内置节点', source: 'inline' }
  const rows = [subscribed, inline]

  assert.deepEqual(filterConfigNodes(rows, ' aLPHa 云 ', 'all'), [subscribed])
  assert.deepEqual(filterConfigNodes(rows, 'Alpha 云', 'inline'), [])
  for (const term of ['大阪', 'EXAMPLE.TEST', 'subscription', '1234']) {
    assert.deepEqual(filterConfigNodes(rows, term, 'subscription'), [subscribed])
  }
  assert.deepEqual(filterConfigNodes(rows, '', 'all'), rows)
})

test('订阅标签跟随对应的 URL，删除、重排或修改草稿时不会错配', async () => {
  const { subscriptionDraftLabels } = await import('../src/lib/subscriptionAttribution.ts')
  assert.deepEqual(subscriptionDraftLabels([alpha, beta], settings), ['Alpha 云', 'Beta 云'])
  assert.deepEqual(subscriptionDraftLabels([beta], settings), ['Beta 云'])
  assert.deepEqual(subscriptionDraftLabels([` ${beta} `, '', alpha, gamma], settings), ['Beta 云', '', 'Alpha 云', ''])
  assert.deepEqual(subscriptionDraftLabels([gamma, beta], settings), ['', 'Beta 云'])
})

test('旧接口、空标签和长度不足的标签数组不会改变 URL 行的位置', async () => {
  const { subscriptionDraftLabels } = await import('../src/lib/subscriptionAttribution.ts')
  assert.deepEqual(subscriptionDraftLabels([alpha, beta, gamma], { subscriptions: [alpha, beta, gamma], subscription_names: ['', 'Beta 云'] }), ['', 'Beta 云', ''])
  assert.deepEqual(subscriptionDraftLabels([alpha, beta], { subscriptions: [alpha, beta] }), ['', ''])
  assert.deepEqual(subscriptionDraftLabels([alpha], { subscription_names: ['Alpha 云'] }), [''])
  assert.deepEqual(subscriptionDraftLabels([alpha], undefined), [''])
})

test('重复的 URL 保留接口提供的同位置标签', async () => {
  const { subscriptionDraftLabels } = await import('../src/lib/subscriptionAttribution.ts')
  const repeated = { subscriptions: [alpha, alpha], subscription_names: ['', '第二个名称'] }
  assert.deepEqual(subscriptionDraftLabels([alpha, alpha, alpha], repeated), ['', '第二个名称', ''])
})

test('后台更新订阅标签不会让设置草稿变成未保存状态', () => {
  const refreshed = { ...settings, subscription_names: ['名称更新', 'Beta 云'] }
  assert.equal(isSettingsDraftDirty(settings, refreshed), false)
  assert.equal(isSettingsDraftDirty({ ...settings, log: { level: 'warn' } }, refreshed), true)
})

test('设置保存仅发送 URL，不发送只读订阅名，也不修改已读取的设置', () => {
  const payload = buildSettingsSavePayload({
    draft: settings,
    serverSettings: settings,
    management: {},
    managementPasswordDraft: '',
    managementPasswordClear: false,
    subscriptions: [beta, gamma],
  })
  assert.deepEqual(payload.subscriptions, [beta, gamma])
  assert.equal(Object.hasOwn(payload, 'subscription_names'), false)
  assert.deepEqual(settings.subscription_names, [' Alpha 云 ', 'Beta 云'])
  assert.equal(isSubscriptionDraftDirty(`${alpha}\n${beta}`, settings.subscriptions), false)
})
