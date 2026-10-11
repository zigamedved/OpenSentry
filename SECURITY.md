# Security policy

## Supported versions

OpenSentry has no numbered releases yet. Security fixes land on the `master` branch. Run the current `master` commit.

| Version | Supported |
| --- | --- |
| `master` | yes |
| older commits | no |

## Reporting a vulnerability

Report vulnerabilities in private. Do not open a public GitHub issue, and do not put exploit details in a pull request.

Use GitHub private vulnerability reporting:

https://github.com/zigamedved/OpenSentry/security/advisories/new

That report goes to the repository maintainers and is not public. Enable private vulnerability reporting in the repository settings if the form is unavailable, or contact [@zigamedved](https://github.com/zigamedved) directly instead of filing a public issue.

Please include the version (git commit), what you expected, and what happened. We will acknowledge the report and follow up with a fix or why we disagree. Give us a chance to publish a fix before any public write-up.

## Sessions, cookies, and XSS

The dashboard signs in with the `opensentry_session` cookie. It is `HttpOnly` and `SameSite=Lax`, and `Secure` when the request is HTTPS or `X-Forwarded-Proto` is `https`. JavaScript on the page cannot read the cookie. A cross-site script on another origin cannot send it either, unless that origin is listed in `CORS_ORIGINS` and the browser is allowed to include credentials.

XSS on the dashboard itself can still call the API as the signed-in user, because the browser will attach the cookie to same-origin requests. It cannot copy the cookie value out to another site.

`POST /api/login` and `POST /api/register` also return the session as JSON `token` for clients that send `Authorization: Bearer`. The dashboard does not store that token. If you keep it in JavaScript, `localStorage`, logs, or a screenshot, XSS (or anyone who can read that store) can steal it and replay it from anywhere. Treat a bearer token like a password. Prefer the cookie for browsers.

`CORS_ORIGINS` lists the only browser origins that may read API responses. The API sets `Access-Control-Allow-Origin` to that exact origin and `Access-Control-Allow-Credentials: true`. It does not use `*`. An empty list means a browser on another origin cannot read the response. Same-origin deployments (nginx in Compose, the Vite proxy) do not need an entry.

Do not put an untrusted site in `CORS_ORIGINS`. JavaScript there can read authenticated responses when the user is signed in and the request includes credentials.

Ping URLs stay callable from cron and `curl` with no `Origin` header and no CORS allowlist entry. The ping token is the secret. A browser on a site that is not allowlisted cannot read the ping response, but knowing the URL is still enough to record a ping. Rotate the URL from the job page if it leaks.
