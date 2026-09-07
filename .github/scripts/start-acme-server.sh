#!/usr/bin/env bash
# Stand up a real ACME server on the runner and make this machine trust it.
#
# Everything about servlo's HTTPS path — the account key, the order, the
# HTTP-01 challenge webroot nginx serves, the 443 vhost, the renewal — has
# never run end to end anywhere. The unit tests drive a fake CA in-process,
# which proves the client speaks the protocol and nothing about whether nginx
# actually answers the authority's knock on port 80.
#
# Pebble is Let's Encrypt's own test server, so this exercises the protocol
# against the same implementation family production uses, with none of its
# rate limits. It is built from source rather than pulled as an image: the
# config has to name the test certificates that ship in the repository, and
# guessing at an image's internal layout is how a CI job spends a week going
# red for reasons that are not the product.
set -euo pipefail

version="v2.7.0"
dir=/tmp/pebble

git clone --depth 1 --branch "$version" https://github.com/letsencrypt/pebble "$dir"
(cd "$dir" && go build -o "$dir/pebble" ./cmd/pebble)

# httpPort 80 is the whole point. Pebble validates HTTP-01 by connecting to the
# domain on this port, so pointing it at 80 puts servlo's nginx — with the
# challenge webroot it generated — on the other end of a real validation.
# Pebble's default is 5002, which nothing serves.
cat > "$dir/servlo-ci.json" <<JSON
{
  "pebble": {
    "listenAddress": "0.0.0.0:14000",
    "managementListenAddress": "0.0.0.0:15000",
    "certificate": "$dir/test/certs/localhost/cert.pem",
    "privateKey": "$dir/test/certs/localhost/key.pem",
    "httpPort": 80,
    "tlsPort": 443,
    "ocspResponderURL": "",
    "externalAccountBindingRequired": false
  }
}
JSON

# Two knobs that only make sense for a test authority. NOSLEEP drops the random
# 0-15s Pebble adds before validating, which is there to catch clients that
# assume it is instant. NONCEREJECT makes it reject 5% of nonces to catch
# clients that do not retry; servlo's client does, and it is tested for that
# elsewhere, so leaving it on here would buy nothing but flakes.
PEBBLE_VA_NOSLEEP=1 PEBBLE_WFE_NONCEREJECT=0 \
  "$dir/pebble" -config "$dir/servlo-ci.json" >/tmp/pebble.log 2>&1 &

# Pebble serves its directory over HTTPS with a certificate from a test root
# that ships in the repository. Trusting it system-wide is what lets servlo's
# ACME client reach the directory at all — Go reads the system pool, and the
# alternative is an environment variable that would only be honoured in CI and
# so would test a code path no server runs.
sudo cp "$dir/test/certs/pebble.minica.pem" /usr/local/share/ca-certificates/pebble-ci.crt
sudo update-ca-certificates

for _ in $(seq 30); do
  if curl -fsS --max-time 5 https://localhost:14000/dir >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! curl -fsS --max-time 5 https://localhost:14000/dir >/dev/null 2>&1; then
  echo "pebble never answered on https://localhost:14000/dir"
  cat /tmp/pebble.log || true
  exit 1
fi

# The root Pebble issues *from* is generated fresh each run and is not the one
# above. A check on the secured site has to trust this one, so write it where
# expect-secured.sh can find it.
curl -fsS https://localhost:15000/roots/0 -o /tmp/pebble-issuing-root.pem
echo "pebble is up; issuing root saved to /tmp/pebble-issuing-root.pem"
