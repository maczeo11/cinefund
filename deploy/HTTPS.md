# HTTPS for the API

Vercel proxies `/api/*` and `/health/*` to the API (see `web/vercel.json`).
That hop must be HTTPS: it carries session tokens. Caddy on the EC2 host
terminates TLS and forwards to the API over localhost.

Do these on the EC2 host **before** deploying the `vercel.json` change, or the
site loses its backend until they are done.

1. Install Caddy: https://caddyserver.com/docs/install
2. Security group: open 80 and 443 to the world (80 is needed for the
   certificate challenge). Close 8080.
3. Run the API bound to localhost only:
   ```
   HTTP_HOST=127.0.0.1
   ```
4. Start Caddy with this repo's config:
   ```
   sudo API_DOMAIN=43-205-27-169.sslip.io caddy run --config deploy/Caddyfile
   ```
   (or copy it to `/etc/caddy/Caddyfile` with the domain filled in and
   `sudo systemctl reload caddy`).
5. Check from outside:
   ```
   curl https://43-205-27-169.sslip.io/health/live
   ```
6. Deploy the web app. `web/vercel.json` already points at that hostname.

With a domain of your own, point an A record at the IP, use it as
`API_DOMAIN`, and update the two destinations in `web/vercel.json`.

The API also needs `DEMO_LOGIN_ENABLED=true` for the demo accounts to work.
