# Coverage roadmap

What a Gateway API linter should cover, and where gwlint is against it.
Core Gateway API first, then implementation-specific checks; see `INSTRUCTION.md` for the original phase plan.

Ordered by how badly the failure hides. A misconfiguration that makes a manifest invalid gets caught by a schema validator.
One that applies cleanly and silently serves nothing is what gwlint is for.

## Tier 1: silent non-attachment

The route exists, the apply succeeds, the status conditions say why, and nobody reads status conditions.
Every one of these ends with traffic going nowhere.

- [ ] `listener-not-found` - a `parentRef` `sectionName` naming a listener the Gateway does not have.
- [ ] `route-not-permitted` - the listener's `allowedRoutes.namespaces` or `allowedRoutes.kinds` excludes the route, so it never attaches.
- [ ] `hostname-never-matches` - the route's hostnames do not intersect the listener's, so no request can ever match.
- [ ] `protocol-mismatch` - an `HTTPRoute` attached to a `TCP` listener, or similar.
- [x] `dangling-parent-ref` - the `parentRef` names a Gateway or ListenerSet that is not present.

## Tier 2: reference integrity

A reference that resolves to nothing. Applies cleanly, fails at request time.

- [x] `dangling-backend-ref` - a `backendRef` naming a Service or Backend that is not present.
- [ ] `missing-reference-grant` - a cross-namespace `backendRef` with no `ReferenceGrant` permitting it.
- [ ] `missing-certificate-grant` - a cross-namespace listener `certificateRef` with no `ReferenceGrant`.
- [ ] `dangling-certificate-ref` - a listener `certificateRef` naming a Secret that is not present.
- [ ] `dangling-gateway-class` - a Gateway naming a `GatewayClass` that is not present.
- [ ] `dangling-listener-set-parent` - a `ListenerSet` whose `parentRef` names a Gateway that is not present.
- [ ] `dangling-extension-ref` - a filter `extensionRef` naming an object that is not present.
- [ ] `dangling-policy-target` - a policy `targetRef` naming an object that is not present.

## Tier 3: route semantics

The route attaches and resolves, but does not do what it looks like it does.

- [ ] `conflicting-route-match` - two routes claiming the same hostname and path on the same listener, with nothing resolving precedence.
- [ ] `all-weights-zero` - every `backendRef` in a rule weighted `0`, so the rule serves nothing.
- [ ] `rule-serves-nothing` - a rule with no `backendRefs` and no terminating filter.
- [ ] `duplicate-rule-name` - two rules on a route sharing a name, which a policy `sectionName` cannot then address unambiguously.
- [ ] `service-backend-without-port` - a `backendRef` to a Service with no port, which Gateway API requires.

## Tier 4: Gateway and listener validity

- [ ] `duplicate-listener-name` - listener names must be unique within a Gateway.
- [ ] `conflicting-listeners` - two listeners on the same port with incompatible protocol or hostname.
- [ ] `tls-listener-without-certificate` - a `Terminate`-mode TLS listener with no `certificateRefs`.
- [ ] `hostname-on-non-hostname-protocol` - a listener hostname on `TCP` or `UDP`, where it means nothing.
- [ ] `gateway-serves-no-routes` - a Gateway no route attaches to.

## Tier 5: Envoy Gateway

Only after the core is covered.

- [x] `fqdn-backend-cold-start` - a policy with no health check covering an FQDN-backed route.
- [ ] `health-check-without-failover` - a health check on a single-endpoint Backend, which changes the failure mode from slow to fast without adding availability.
- [ ] `retry-without-budget` - retries with no budget or cap, or on non-idempotent methods with no idempotency guard.
- [ ] `backend-without-endpoints` - a `Backend` with an empty `endpoints` list.
- [ ] `conflicting-policies` - two policies of the same kind targeting the same object, where only one takes effect.

## Not checks

Out of scope by design, because something else already does them:

- Schema validity. That is kubeconform's job.
- Workload-level checks such as "container runs as root". That is kube-linter's, and gwlint imports its engine rather than duplicating it.
