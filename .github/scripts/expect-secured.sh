#!/usr/bin/env bash
# Assert a site actually answers over HTTPS, with the certificate the authority
# just issued, through nginx's 443 vhost.
#
# --resolve rather than a hosts entry, and --cacert rather than -k. Skipping
# verification would pass against any certificate at all, including the
# self-signed placeholder a site carries before it is secured, which is exactly
# the failure this is here to catch.
set -euo pipefail

domain="$1"
want="servlo-ci:$domain"
root=/tmp/pebble-issuing-root.pem
ip=$(ip -4 -o route get 1.1.1.1 2>/dev/null | awk '{for (i=1;i<NF;i++) if ($i=="src") print $(i+1)}')

report() {
  echo "last body: ${body:-<nothing>}"
  curl -sS -i --max-time 10 --cacert "$root" --resolve "$domain:443:$ip" "https://$domain/" || true
  echo "--- certificate as served"
  echo | openssl s_client -connect "$ip:443" -servername "$domain" 2>/dev/null |
    openssl x509 -noout -subject -issuer -dates || true
  echo "--- pebble"
  tail -50 /tmp/pebble.log || true
  echo "--- nginx"
  podman logs servlo-nginx 2>&1 | tail -50 || true
}

body=""
for _ in $(seq 20); do
  body=$(curl -fsS --max-time 10 --cacert "$root" --resolve "$domain:443:$ip" "https://$domain/" 2>/dev/null || true)
  [ "$body" = "$want" ] && break
  sleep 2
done

if [ "$body" != "$want" ]; then
  echo "$domain did not serve its own page over a verified HTTPS connection"
  report
  exit 1
fi
echo "$domain serves over HTTPS with a certificate this machine verified"

# The issuer is checked as well as the handshake. A self-signed certificate
# would verify against nothing, but a stale one from an earlier run of this job
# would verify against the same root while proving that nothing was reissued.
issuer=$(echo | openssl s_client -connect "$ip:443" -servername "$domain" 2>/dev/null |
  openssl x509 -noout -issuer)
echo "issuer: $issuer"
case "$issuer" in
  *Pebble*) ;;
  *) echo "expected a Pebble-issued certificate, got: $issuer"; report; exit 1 ;;
esac

# A secured site 301s port 80 to 443 — except under the challenge prefix, which
# stays plain http because that is where the *next* renewal will be validated.
# The vhost template says as much in a comment; nothing checked it, and getting
# it wrong is invisible for ninety days and then takes every site down at once.
code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 10 --resolve "$domain:80:$ip" "http://$domain/" || true)
if [ "$code" != "301" ]; then
  echo "expected http://$domain/ to redirect to https, got $code"
  exit 1
fi
echo "http://$domain/ redirects to https"

probe=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 10 \
  --resolve "$domain:80:$ip" "http://$domain/.well-known/acme-challenge/servlo-ci-probe" || true)
if [ "$probe" = "301" ]; then
  echo "the challenge path redirects to https, so no renewal on this site can ever be validated"
  exit 1
fi
echo "the challenge path is still served over plain http (answered $probe)"
