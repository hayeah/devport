# Tailscale Services Auto-Approval

## Problem

`tailscale serve --service svc:<name>` registers a service, but it requires admin approval before DNS/VIP is assigned. Auto-approval via `autoApprovers` in the ACL policy doesn't support wildcards (`svc:*`).

## What Works

- **Per-service ACL entry**: `"svc:d7e": ["tag:services"]` — auto-approves that specific service for devices tagged `tag:services`
- **Tag-based**: `"tag:devport": ["tag:services"]` — auto-approves any service tagged `tag:devport`, but services must be pre-defined in the admin console with that tag

## What Doesn't Work

- `"svc:*": ["tag:services"]` — wildcards are not supported
- Dynamically created services (like devport's hash-based names) can't be auto-approved without either:
  - Pre-registering each service in the admin console
  - Adding per-service ACL entries via the Tailscale API

## Options to Explore

- **Tailscale API**: programmatically add `autoApprovers` entries or approve services via API after registration
- **Single shared service**: use one fixed service name (e.g. `svc:devport`) and route by path or port instead of separate VIPs
- **Manual approval**: approve once per service in admin console — persists across restarts since devport reuses svc prefix
- **Funnel**: different mechanism, might have different approval flow

## References

- https://tailscale.com/kb/1552/tailscale-services
- https://tailscale.com/blog/services-beta
