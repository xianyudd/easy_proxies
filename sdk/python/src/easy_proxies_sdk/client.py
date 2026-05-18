from __future__ import annotations

import json
from typing import Any, Dict, List, Optional
from urllib.error import HTTPError
from urllib.parse import urlencode
from urllib.request import Request, urlopen


class EasyProxiesError(RuntimeError):
    def __init__(self, message: str, status: int, payload: Any = None) -> None:
        super().__init__(message)
        self.status = status
        self.payload = payload


class EasyProxiesClient:
    def __init__(
        self,
        base_url: str = "http://127.0.0.1:9091",
        token: Optional[str] = None,
        password: Optional[str] = None,
        timeout: float = 30.0,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self.token = token
        self.password = password
        self.timeout = timeout

    def login(self, password: Optional[str] = None) -> Dict[str, Any]:
        password = password if password is not None else self.password
        if not password:
            raise ValueError("password is required")
        result = self._request("POST", "/api/auth", body={"password": password})
        token = result.get("token")
        if token:
            self.token = str(token)
        return result

    def extract(
        self,
        region: str = "all",
        mode: str = "pool",
        format: str = "http_url",
        count: int = 1,
        reveal: bool = False,
    ) -> Dict[str, Any]:
        return self._get(
            "/api/extractor",
            {
                "region": region,
                "mode": mode,
                "format": format,
                "count": str(count),
                "reveal": "true" if reveal else None,
            },
        )

    def get_proxy_urls(
        self,
        region: str = "all",
        mode: str = "pool",
        count: int = 1,
        reveal: bool = False,
    ) -> List[str]:
        data = self.extract(
            region=region,
            mode=mode,
            format="http_url",
            count=count,
            reveal=reveal,
        )
        return [str(item) for item in data.get("entries", [])]

    def get_proxy_entries(
        self,
        region: str = "all",
        mode: str = "multi-port",
        count: int = 1,
        reveal: bool = False,
    ) -> List[Dict[str, Any]]:
        data = self.extract(
            region=region,
            mode=mode,
            format="json",
            count=count,
            reveal=reveal,
        )
        return [item for item in data.get("entries", []) if isinstance(item, dict)]

    def get_nodes(self) -> Dict[str, Any]:
        return self._get("/api/nodes")

    def get_cloudflare_cache(self) -> Dict[str, Any]:
        return self._get("/api/cloudflare/cache")

    def get_reputation_cache(self) -> Dict[str, Any]:
        return self._get("/api/reputation/cache")

    def check_cloudflare(
        self,
        region: str = "all",
        count: int = 20,
        include_unavailable: bool = False,
        retry_failed: bool = False,
        timeout: Optional[float] = None,
    ) -> Dict[str, Any]:
        return self._get(
            "/api/cloudflare/check",
            {
                "region": region,
                "mode": "multi-port",
                "count": str(count),
                "include_unavailable": "true" if include_unavailable else None,
                "retry_failed": "true" if retry_failed else None,
            },
            timeout=timeout,
        )

    def check_reputation(
        self,
        region: str = "all",
        count: int = 20,
        include_unavailable: bool = False,
        retry_failed: bool = False,
        timeout: Optional[float] = None,
    ) -> Dict[str, Any]:
        return self._get(
            "/api/reputation/check",
            {
                "region": region,
                "mode": "multi-port",
                "count": str(count),
                "include_unavailable": "true" if include_unavailable else None,
                "retry_failed": "true" if retry_failed else None,
            },
            timeout=timeout,
        )

    def _get(
        self,
        path: str,
        query: Optional[Dict[str, Optional[str]]] = None,
        timeout: Optional[float] = None,
    ) -> Dict[str, Any]:
        params = {key: value for key, value in (query or {}).items() if value not in (None, "")}
        suffix = f"?{urlencode(params)}" if params else ""
        return self._request("GET", f"{path}{suffix}", timeout=timeout)

    def _request(
        self,
        method: str,
        path: str,
        body: Optional[Dict[str, Any]] = None,
        timeout: Optional[float] = None,
    ) -> Dict[str, Any]:
        headers = {"Accept": "application/json"}
        data = None
        if body is not None:
            data = json.dumps(body).encode("utf-8")
            headers["Content-Type"] = "application/json"
        if self.token:
            headers["Authorization"] = f"Bearer {self.token}"

        request = Request(
            f"{self.base_url}{path}",
            data=data,
            headers=headers,
            method=method,
        )

        try:
            with urlopen(request, timeout=timeout or self.timeout) as response:
                return self._decode_response(response.read(), response.headers.get("Content-Type", ""))
        except HTTPError as error:
            payload = self._decode_response(error.read(), error.headers.get("Content-Type", ""))
            message = payload.get("error") if isinstance(payload, dict) else None
            raise EasyProxiesError(message or f"Easy Proxies request failed with HTTP {error.code}", error.code, payload) from error

    @staticmethod
    def _decode_response(raw: bytes, content_type: str) -> Dict[str, Any]:
        text = raw.decode("utf-8", errors="replace")
        if "application/json" not in content_type:
            return {"text": text}
        if not text:
            return {}
        parsed = json.loads(text)
        return parsed if isinstance(parsed, dict) else {"data": parsed}
