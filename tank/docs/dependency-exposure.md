# npm dependency exposure

Review date: 2026-10-23 (full frontend, production-only frontend and full SDK audits
repeated). This is targeted dependency triage, not an independent
security audit or a statement that all application vulnerabilities are absent.

## Production framework advisories patched

The October 23 refresh newly reported one critical production package entry for
Next 16.3.8, covering three advisories published to the reviewed database on
October 22: [image optimization](https://github.com/advisories/GHSA-2464-fhr4-w9xw),
[App Router Server Actions denial of service](https://github.com/advisories/GHSA-p3m5-59mp-jv5r),
and [typed-array cache functions](https://github.com/advisories/GHSA-673q-mjm6-r5xr).
All identify 16.4.1 as a patched 16.x version. Tank now pins Next and its matching
lint configuration to 16.4.2, retaining Node 24 and React 19.2.8. The upgraded
framework requires sharp `^0.35.5`; the lockfile's already-resolved sharp 0.35.5
remains unchanged. Version-based findings do not establish which advisory
conditions were reachable in Tank; no exploit/reachability claim is made.

After the compatible upgrade, the frontend production-only audit reports zero,
the full frontend audit retains five tooling entries below, and the SDK audit
reports zero. Browser CI now gates on `npm audit --omit=dev --audit-level=low`
after installation. That gate deliberately checks the production graph; the
unresolved development findings remain visible and documented, not suppressed.
Runtime images must be rebuilt from the updated lockfile; changing source does
not patch a previously built/deployed image. No hosted deployment is claimed.

## Unresolved development advisory

The frontend lockfile reports five high-severity package entries, all leading to
one underlying advisory, [GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm)
(CVE-2026-93687). The path is:

`eslint-config-next -> @next/eslint-plugin-next -> fast-glob -> micromatch -> braces`

`braces <=3.0.3` can exhaust the call stack when processing deeply nested brace
patterns. The official advisory lists no patched version as of this review.
The audit's proposed forced downgrade to eslint-config-next 14.2.35 is not applied:
Tank uses Next 16.4.2 and retains its matching lint configuration. There is no
dependency override that merely relabels this issue as fixed.

| Boundary | Evidence and limitation |
| --- | --- |
| Developer/build environment | All five package entries remain installed and reported. Linting/building untrusted repository changes remains an exposure boundary; never put deployment custody or production credentials in this environment. |
| Production dependency graph | `npm audit --omit=dev --json` reports zero findings for the current frontend lockfile. This alone does not prove which files an image contains. |
| Previous container packaging | The runtime copied the complete build workspace, including developer dependencies. Thus development-only classification was insufficient to exclude them from the image. |
| Updated container packaging | Next standalone output tracing selects runtime files. The runtime copies only that artifact, public assets, and generated static assets, removes inherited npm/npx/corepack and Yarn package managers, and executes its generated server. A build-time inventory rejects the lint chain and unnecessary TypeScript, Playwright, and Tailwind tooling in the actual artifact. |
| Application request handling | Source review found no direct import of braces/micromatch/fast-glob in Tank's API/browser application source. This is limited reachability evidence, not proof about every transitive or compiled code path. |

No audit findings are suppressed. Recheck full and production-only reports when
dependency locks change. Update compatible packages when a real upstream fix is
available and rerun lint/build/runtime inventory/browser checks.

## Reproduce

From `tank/frontend`, after building the linked SDK and installing dependencies:

```bash
npm audit --json
npm audit --omit=dev --json
TANK_STANDALONE_BUILD=1 npm run build
node scripts/check-runtime.mjs
npm test
npm run lint
```

The full audit currently exits nonzero because the tooling advisory is unresolved.
Do not use `npm audit fix --force`. The standalone artifact inventory fails the
container build if excluded package files reappear, including nested dependencies
or symlinks escaping the runtime artifact.

The artifact inventory covers application packages, not all operating-system
libraries in the pinned base. Container CI additionally inspects the final image
for removal of inherited package managers before exercising the server. An
expanded final-image inspection found `/opt/yarn-v1.22.22` and its two executable
links, missed by the original npm/corepack-only probe. The runtime now removes
that unused payload too; CI rejects Yarn directories, package-manager links
(including dangling links) and any remaining global Node packages. The expanded
probe rejects the previous image and passes on the rebuilt image. This is
not a full operating-system vulnerability scan or an independent audit.
