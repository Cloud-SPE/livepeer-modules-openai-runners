# TRUST-MODEL

What runners see vs. what the broker handles. The defensive posture summary
is in [`SECURITY.md`](./SECURITY.md); this is the detail.

## Layered responsibility

```text
┌────────────────────────────────────────────────────────────────────┐
│ gateway tier                                                       │
│   - API key auth                                                   │
│   - Wire-protocol middleware (OpenAI shape mapping, rate limits)   │
│   - Customer identity propagation                                  │
└──────────────────────────┬─────────────────────────────────────────┘
                           │
                           ▼
┌────────────────────────────────────────────────────────────────────┐
│ capability broker                                                  │
│   - Payment envelope validation (via payment-daemon)               │
│   - Capability dispatch (route to a runner per host-config)        │
│   - Mode dispatch + extractor logic (work-unit accounting)         │
│   - Strips identity headers before forwarding                      │
└──────────────────────────┬─────────────────────────────────────────┘
                           │  Livepeer-Capability + Livepeer-Offering only
                           ▼
┌────────────────────────────────────────────────────────────────────┐
│ runner (this repo)                                                 │
│   - Inference / model serving                                      │
│   - Work-unit reporting (body field or trailer/header)             │
│   - Metrics + health                                               │
└────────────────────────────────────────────────────────────────────┘
```

## What runners see

- HTTP method + path + body — fully validated by the gateway and broker.
- `Livepeer-Capability` header — informational.
- `Livepeer-Offering` header — informational.

That's the whole input surface. The runner has zero knowledge of the customer
making the request.

## What runners do NOT see

- Customer identity, API keys, or session tokens.
- Payment envelopes or signed-quote tickets.
- Authorization headers (the broker strips these before forwarding).
- Routing metadata (region, multi-tenant tags, billing plan).
- Rate-limit state.

## Why this matters

**Compromise blast radius is bounded.** A compromised runner cannot read
customer identity, cannot issue payments, cannot escalate to gateway-tier
secrets. The worst it can do is produce bad inference outputs for the requests
it sees.

**Easier to swap implementations.** A new runner can be dropped in without
touching auth, billing, or routing logic.

**Easier to reason about.** When debugging a request, the runner's behavior is
purely a function of the request body — no hidden state, no identity-coupled
branches.

## Implications for runner code

- Don't read `Authorization` headers. If you see one, you've found a broker bug.
- Don't persist anything keyed by `Livepeer-Capability` or `Livepeer-Offering`
  beyond a single request lifetime (those are not stable identifiers in any
  customer sense).
- Don't make outbound calls back to the broker or gateway. Runners are
  request-response only.
- Treat `Livepeer-*` headers as logging labels, not auth.
