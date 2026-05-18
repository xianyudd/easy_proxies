# Easy Proxies Python SDK

Zero-dependency Python SDK for other Python projects to fetch proxies from a running Easy Proxies instance.

## Install

From this repository:

```bash
pip install /path/to/easy_proxies/sdk/python
```

For editable local development:

```bash
pip install -e /path/to/easy_proxies/sdk/python
```

## Get Proxy URLs

```python
from easy_proxies_sdk import EasyProxiesClient

client = EasyProxiesClient(base_url="http://127.0.0.1:9091")

proxies = client.get_proxy_urls(
    region="jp",
    mode="multi-port",
    count=5,
    reveal=True,
)

print(proxies[0])
```

## Use With requests

Install requests in your own project:

```bash
pip install requests
```

```python
import requests
from easy_proxies_sdk import EasyProxiesClient

client = EasyProxiesClient("http://127.0.0.1:9091")
proxy = client.get_proxy_urls(region="jp", mode="multi-port", count=1, reveal=True)[0]

res = requests.get(
    "https://api.ipify.org",
    proxies={"http": proxy, "https": proxy},
    timeout=15,
)

print(res.text)
```

## Structured Entries

```python
entries = client.get_proxy_entries(
    region="us",
    mode="multi-port",
    count=10,
    reveal=True,
)

for entry in entries:
    print(entry["node_name"], entry["url"])
```

## Read Quality Cache

```python
cf = client.get_cloudflare_cache()
reputation = client.get_reputation_cache()

print(cf["count"], reputation["count"])
```

## Trigger Full Scans

```python
client.check_cloudflare(
    region="all",
    count=500,
    include_unavailable=True,
    timeout=600,
)

client.check_reputation(
    region="all",
    count=500,
    include_unavailable=True,
    timeout=600,
)
```

## Password-Protected WebUI

```python
import os
from easy_proxies_sdk import EasyProxiesClient

client = EasyProxiesClient(
    base_url="http://127.0.0.1:9091",
    password=os.environ["EASY_PROXIES_PASSWORD"],
)

client.login()
nodes = client.get_nodes()
```

## Security Notes

- `reveal=True` returns real proxy credentials.
- Do not expose `9091` publicly without `management.password`.
- Prefer `127.0.0.1` or a private network for the management API.
