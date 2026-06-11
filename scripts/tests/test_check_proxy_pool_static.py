from pathlib import Path

SRC = Path(__file__).resolve().parents[2] / "check_proxy_pool.sh"


def read_source() -> str:
    return SRC.read_text()


def test_geoip_checks_use_username_suffix_not_proxy_path():
    text = read_source()
    assert '"${LISTENER_USER}-${region}:${LISTENER_PASS}"' in text
    assert '"http://${GEOIP_ADDR}:${GEOIP_PORT}"' in text
    assert 'http://${GEOIP_ADDR}:${GEOIP_PORT}/${region}/' not in text
    assert '用户名后缀选区' in text


def test_proxy_user_can_be_overridden_per_probe():
    text = read_source()
    assert 'local proxy_user="${3:-$LISTENER_USER:$LISTENER_PASS}"' in text
    assert '--proxy-user "$proxy_user"' in text


if __name__ == "__main__":
    test_geoip_checks_use_username_suffix_not_proxy_path()
    test_proxy_user_can_be_overridden_per_probe()
