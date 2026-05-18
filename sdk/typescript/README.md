# Easy Proxies TypeScript SDK

Lightweight SDK for other projects to fetch and test proxies from a running Easy Proxies instance.

## Install

This package is currently stored in-repo:

```bash
npm install /path/to/easy_proxies/sdk/typescript
```

## Basic Usage

```ts
import { EasyProxiesClient } from '@easy-proxies/sdk'

const client = new EasyProxiesClient({
  baseUrl: 'http://127.0.0.1:9091',
})

const proxies = await client.getProxyUrls({
  region: 'jp',
  mode: 'multi-port',
  count: 5,
  reveal: true,
})

console.log(proxies[0])
```

Use with `fetch`:

```ts
const [proxy] = await client.getProxyUrls({ region: 'us', mode: 'pool', reveal: true })

// Most HTTP clients need their own proxy-agent package. This SDK only returns
// proxy endpoints and does not force a specific HTTP stack.
console.log(`Use proxy: ${proxy}`)
```

## Structured Entries

```ts
const entries = await client.getProxyEntries({
  region: 'hk',
  mode: 'multi-port',
  count: 10,
  reveal: true,
})

for (const entry of entries) {
  console.log(entry.node_name, entry.url)
}
```

## Quality Cache

```ts
const cf = await client.getCloudflareCache()
const reputation = await client.getReputationCache()

console.log(cf.count, reputation.count)
```

## Trigger Scans

```ts
await client.checkCloudflare({
  region: 'all',
  count: 500,
  includeUnavailable: true,
})

await client.checkReputation({
  region: 'all',
  count: 500,
  includeUnavailable: true,
})
```

## Password-Protected WebUI

If `management.password` is configured:

```ts
const client = new EasyProxiesClient({
  baseUrl: 'http://127.0.0.1:9091',
  password: process.env.EASY_PROXIES_PASSWORD,
})

await client.login()
const nodes = await client.getNodes()
```

## Security Notes

- `reveal: true` returns real proxy credentials.
- Do not expose `9091` publicly without `management.password`.
- Prefer binding the management API to `127.0.0.1` or a private network.
