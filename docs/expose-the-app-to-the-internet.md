# Expose the check-in app to the internet

Volunteers open a URL in Chrome on their phone and see the checkouts board. Nothing is installed on their devices. This guide covers the DNS failure that breaks Android today, the Cloudflare Tunnel setup that fixes it, and the changes to make before the app answers requests from the internet.

The app currently runs on Dokploy behind Traefik, and staff reach it at `kids-checkin.local`.

## Why `kids-checkin.local` fails on Android

RFC 6762 reserves `.local` for multicast DNS. macOS, iOS, and Linux resolve those names through an mDNS responder. Android exposes mDNS discovery to apps through `NsdManager`, but its system resolver and Chrome do not use it to look up hostnames. Chrome on Android therefore reports `ERR_NAME_NOT_RESOLVED` before it opens a connection.

The app never sees the request. The Go server, the session cookie, and HTTP versus HTTPS are not involved.

mDNS is also link-local. The name fails across subnets, on guest Wi-Fi, and over cellular, so a phone on another network cannot resolve it even on a platform that supports mDNS.

`avahi-resolve -n kids-checkin.local` fails with a resolution error when no host on the link advertises the name.

## Why Cloudflare Tunnel

`cloudflared` runs on the Dokploy host and opens outbound connections to Cloudflare. Cloudflare maps one public hostname to one local service. You do not need a public IP or an inbound port, and the tunnel exposes only the hostname-to-service mapping you add.

The trade-offs fit this app:

- Volunteers install nothing. Access uses a browser login.
- The app already depends on outbound internet for Planning Center. The tunnel adds an inbound dependency on Cloudflare.
- Cloudflare terminates TLS, so `window.isSecureContext` is true, the app can be added to the home screen, and `navigator.clipboard` works.
- Port forwarding would expose a port on the host. A public IP with Let's Encrypt in Dokploy is simpler only when the host has a routable address.

Access is part of this plan, not an optional extra. Without it the app is a shared password, two known usernames, and no per-user revocation.

Do not add private network routes to this tunnel. Those routes are what would give it a path to the rest of the network.

## Before the migration

These changes need no tunnel and no public hostname. Do them first.

1. Delete the WebSocket branch. No client uses it, but the route still accepts upgrades at `internal/controllers/checkinv1/checkin.go:70`, and `wsClients` is written from every connection goroutine at `checkin.go:250` and deleted at 273, 298, 311, and 332 without a lock. Two upgrades can cause a fatal concurrent map write, which `recover.New()` cannot catch. The log line at `checkin.go:260` records an empty cookie, because Fiber's cookie is named `session_id` and the code reads `session`. Delete:
   - the branch at `checkin.go:245` and the dispatch at `checkin.go:89-90`
   - `wsClients` at `checkin.go:49` and its constructor entry at `checkin.go:62`
   - `c.Locals("allowed", ...)` at `checkin.go:88`, which only the deleted branch reads
   - `wsClient`, `CheckinFilter`, and `websocketQueryShouldBreak`
   - `Test_websocketQueryShouldBreak` and the test at `internal/controllers/middleware/compress_test.go:141-162`
   - the imports at `checkin.go:5`, `checkin.go:13`, `checkin.go:31`, `internal/controllers/middleware/compress.go:6`, and `checkin_test.go:6`
   - the check at `internal/controllers/middleware/compress.go:19`

   Run `go mod tidy`, then `go test ./...` and `go test -tags dev ./...`. Update the WebSocket mentions in `README.md:3`, `README.md:277`, and `AGENTS.md:131`.
2. Rate-limit `POST /login`. No limiter is registered today, and `PostLogin` at `internal/controllers/login/login.go:59` goes straight to `bcrypt.CompareHashAndPassword`. Add an in-memory limiter to the `/login` route: count attempts per client, return 429 after a threshold, and reset the window after a cooldown. The app is a single instance, so the counter can live in memory and reset on restart.
3. Resolve the real client IP for the limiter. Behind the tunnel, `c.IP()` is the connector address. Use `CF-Connecting-IP` only when the socket peer is the cloudflared container, and use `c.IP()` otherwise. Do not set `ProxyHeader` globally. Use a sliding delay or a short window, not a lockout: every device on church Wi-Fi shares one egress IP, so a lockout lets one person lock out the staff.
4. Session regeneration is implemented. `PostLogin` calls `sess.Regenerate()` at `internal/controllers/login/login.go:76` before setting the values, so a session ID fixed before login cannot become authenticated.
5. Unregister the guest check-in routes. The `guestcheckinv1` controller registers `/guest-checkin`, `/v1/checkins/guest-submissions*`, and `/admin/guest-entries`, but the app does not use them. Remove the `NewController` and `RegisterRoutes` calls from `server.go`, remove the Guest entries card from `internal/web/static/pages/admin/index.html:36-43`, and add a test that the paths return 404. If you keep the feature instead, do all of this first: move the public POST off `/v1/checkins/guest-submissions` so it no longer shares a path with the authenticated list, add an Access Bypass scoped to the exact public paths and `/static/*`, gate the status and check-in writes on admin (`guest_submission.go:56-57`, and `isValidTransition` ignores `isAdmin`), and verify a Turnstile token in `CreateSubmission`.
6. `COOKIE_SECURE` is now an explicit setting. The session config at `internal/controllers/server.go:63-69` reads it for the cookie's `Secure` flag and defaults it to off, so the current HTTP deployment kept working. Leave it false until every browser URL is `https://`, then set `COOKIE_SECURE=true` in Dokploy and redeploy. It was deliberately not gated on `ENVIRONMENT` or `static.IsDev()`, so a TLS-terminating proxy in front of a plain-HTTP origin remained allowed.
7. Replace the username select. `internal/web/static/pages/login/index.html:45-54` lists `user` and `admin` in a `<select>`. Use a text input, or accept that the account names are public.
8. Stop echoing internal errors. `server.go:97` puts the error message in the JSON response with `fmt.Sprintf`, and handlers pass `err.Error()`, for example at `checkin.go:164`. The result is a leak and often invalid JSON with a `text/plain` content type. Return a generic message for 5xx responses with `c.JSON` and log the detail.
9. `X-Robots-Tag: noindex` is implemented in `internal/controllers/middleware/noindex.go` and applies to every response as a second guard behind Access.
10. Decide the session lifetime and how to revoke one. The app keeps a session for 180 days (`server.go:63`) and has no server-side revocation. Shorten it if staff share tablets, rotate `LOGIN_PASSWORD_USER` when a volunteer leaves, and treat a lost phone as a password change plus a session purge.
11. Cap the checkouts query. The API applies `limit` only when the client sends it (`internal/repo/checkin/checkin.go:143`), and `checked_out_after` accepts any window (`checkin.go:449-459`). An authenticated user can omit both and read the whole table. Apply a default limit and reject windows older than a shift.
12. Use `Cache-Control: private, no-store` for the checkouts JSON and HTML. `no-cache` at `checkin.go:206` lets a shared tablet keep names and security codes in its HTTP cache after logout.
13. Set `ENVIRONMENT=production` in Dokploy and confirm `/static/dev/preview.js` returns 404. `.env.example:12` defaults to `dev`, which serves dev assets and injects the preview script.
14. Log the session username on state-changing requests. The request log at `internal/controllers/middleware/requestlogger.go:50-59` records method, path, status, IP, and user agent, but not who confirmed a checkout or created a manual check-in.
15. Confirm the Planning Center key stays server-side. It already is. No browser script contains the key or an auth header.

## During the migration

Create the Access application before the public hostname exists. Access applies as soon as the hostname is proxied, so this order keeps the app private from the first request.

1. Delegate the domain's nameservers to Cloudflare. Wait for the zone to show Active.
2. Create an Access application for `checkin.example.org`. The hostname does not need to resolve yet.
3. Add an Allow policy with the staff email addresses, or a domain the church controls. Never use a public provider such as gmail.com, because that admits everyone on it.
4. Choose One-time PIN, or Google Workspace SSO if you have it.
5. Set the application session duration to one week. The global session duration applies to every Access app in the account, so change it only if that is intended.
6. Delete any existing A, AAAA, or CNAME record for `checkin.example.org`. The tunnel fails to add a hostname that already has one.
7. In Zero Trust, open Networks then Tunnels, and create a tunnel named `kids-checkin`.
8. Add the public hostname `checkin.example.org`. Access already covers it.
9. Point the tunnel at the app container, not Traefik. Attach `cloudflared` to a network that contains only the app and the connector, and target `http://<container>:3000`. A shared Dokploy network lets a compromised connector reach other containers.
10. Remove the app's Dokploy domain and Traefik route, so the tunnel is the only path. If you keep a LAN path, treat it as a separate trust zone: allow only the LAN subnet, terminate TLS there, and accept that Access does not cover it.
11. Run `cloudflared` with `restart: unless-stopped`. Copy the token from the tunnel's install page and store it as a Dokploy secret. Dokploy injects secrets as environment variables, so `cloudflared` reads `TUNNEL_TOKEN` without the token appearing in the compose file or the container command line. Anyone with Docker or host access can still read the environment. Mount a file and use `--token-file` if that matters.
12. Close inbound 80, 443, and the Dokploy panel port in the provider firewall or the Docker `DOCKER-USER` chain. Docker-published ports bypass `ufw` INPUT rules. Reach Dokploy over its own tunnel or from the LAN. Allow outbound TCP and UDP 7844 to Cloudflare. Verify with `docker port <container>` and a request from outside the LAN.
13. In Cloudflare, add a rate-limiting rule for `/login`. The free plan includes one rule. Verify it is available on your plan and keep the app limiter as the fallback. Bot Fight Mode applies to the whole zone and can challenge the 3-second poll, so test the board after enabling it or use a WAF rule scoped to `/login`.

The Access free plan includes 50 users. The checkout fetcher makes outbound calls to Planning Center, so Access does not affect it. Any external client that calls this app's API needs an Access service token.

No bypass is needed once the guest routes are removed. If you keep them, add a Bypass application for `/guest-checkin`, `/static/*`, and the public submission path, and verify the kiosk in an incognito window before trusting it.

The container side looks like this. Replace `app-network` with a dedicated network that holds only the app and the connector.

```yaml
services:
  cloudflared:
    image: cloudflare/cloudflared:latest
    command: tunnel --no-autoupdate run
    environment:
      - TUNNEL_TOKEN
    restart: unless-stopped
    networks:
      - app-network

networks:
  app-network:
    external: true
```

`TUNNEL_TOKEN` comes from the Dokploy secret. Pin the image by version or digest instead of `latest`.

## After the migration

1. Check the result. Without an Access session, `curl -I https://checkin.example.org/login` returns 302, 401, or 403 from Access, not 200. A 200 means the request reached the origin and Access is not covering the hostname. Then complete both logins in a browser and confirm the board loads. Confirm the origin is not reachable from the internet by host IP or by the old Dokploy domain.
2. Decide the LAN fallback. If you need one, give it TLS and keep `COOKIE_SECURE=true`. A plain-HTTP fallback sends the 180-day session cookie in cleartext to anyone on the Wi-Fi. If you do not need one, leave the port closed and the Dokploy domain removed.
3. Set `COOKIE_SECURE=true` in Dokploy and redeploy once every path is HTTPS. Log in once from a phone to confirm the cookie is accepted.
4. Test the Access expiry path. When an Access session expires, the poll fetch at `checkouts.js:1101`, scheduled every 3 seconds at `checkouts.js:1492`, gets a redirect or an error instead of JSON. `fetch` follows the redirect, so check `response.redirected` or the content type and reload the page. Do not rely on `response.ok`.
5. Test Access logout on a shared tablet. The app logout at `login.go:109` ends only the app session. Link `https://<team>.cloudflareaccess.com/cdn-cgi/access/logout` or tell staff to use it.
6. Update QR codes, bookmarks, and home-screen shortcuts to the new hostname. The old `.local` URL does not work on Android.
7. Watch the tunnel through the first service. With no connector connected, Cloudflare returns error 1033. A 502 means a connector is up but the app is not answering.

## What stays private

- Existing check-in data reaches only an authenticated session. The checkouts UI puts the location group name in the URL (`home.js:11`), so proxy logs will contain group names. Children's names stay out of URLs unless a client sends the `first_name` or `last_name` filters at `checkin.go:395-396`.
- Cloudflare is in the data path and can see response bodies.
- The app logs `c.IP()` at `internal/controllers/middleware/requestlogger.go:55`. Behind the tunnel that is the connector address, not the volunteer's IP. Set `ProxyHeader` only if you trust every path to the app.
- The session cookie is `HttpOnly` and `SameSite=Lax`. `Secure` adds the HTTPS-only rule.
- The SQLite file holds children's PII, parent contact data, and live session rows, because the session store uses the same file (`server.go:48-58`). Back it up encrypted, restrict who can read the volume, test a restore, and set a retention period. Guest rows have no production delete path, so purge them if the feature stays off.
- The tunnel exposes one hostname and one service. It adds no private network routes.
- App routes that need no app session are `/`, `/login`, `/v1/location_groups`, `/api/session`, `/static/*`, and the manifest and icons. `/api/session` reports `authenticated` and `role`. `/v1/location_groups` returns group names. Neither returns child data.

## Operate it

- Access is mandatory for the public hostname. Do not disable it to make a check pass.
- Rotate `LOGIN_PASSWORD_USER` when a volunteer leaves. There is no per-user revocation.
- Sessions live in the SQLite file. If the app moves without that file, every user logs in again.
- An expired Access session leaves the board stale until someone reloads. Keep the Access session longer than a shift.
- Changing the hostname changes the origin, so every user logs in again. Update QR codes, bookmarks, and home-screen shortcuts.
- If the app moves, change the tunnel target and keep the hostname. Users keep their sessions if the database moves too.
