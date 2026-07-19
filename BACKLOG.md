# Suivi du backlog Dockyard

> Fichier de suivi pour reprendre l'exécution du backlog dans une nouvelle session.
> Source : les 47 issues GitHub (https://github.com/KGMA74/dockyard/issues), créées le 2026-07-18.
> Workflow par tâche : implémenter → tester localement → lint → commit → push → fermer l'issue GitHub avec référence du commit → vérifier la CI → mettre à jour ce fichier + le README si le comportement visible change.
> Contraintes : jamais de trailer Co-Authored-By dans les commits ; commit + push autorisés sans demander.

## État d'avancement

| Tâche | Issue | Statut | Commit | Notes |
|---|---|---|---|---|
| T.1 — CI quality gate | #1 | ✅ fait | `2cded18` | ci.yml (vet+lint+test -race+build UI), .golangci.yml, make lint ; 35 errcheck corrigés dont bug AppendUpload |
| T.2 — Harness de tests | #2 | ✅ fait | `ec98276` | storagetest.RunBackendContract (S3 gated par DOCKYARD_TEST_S3_ENDPOINT), tests auth/v2/GC ; bug Windows PutBlob corrigé |
| P1.1 — Scaffold SQLite | #3 | ✅ fait | `8472178` | internal/store, modernc.org/sqlite, migrations go:embed, dockyard.db dans les 2 modes |
| P1.2 — RBAC multi-users | #4 | ✅ fait | `b9f1e01` | rôles admin/pusher/reader + globs, CRUD /users, Authorize(), RequireAdmin sur DELETE+gc, migration admin legacy |
| P1.3 — Sessions/refresh | #5 | ✅ fait | `fd544aa` | access 15 min + refresh 30 j rotation single-use, /auth/refresh, /sessions list+revoke, blacklist persistée, JWT_SECRET_PREVIOUS, intercepteur UI |
| P1.4 — Docker token auth /v2/token | #6 | ✅ fait | `28cf129` | /v2/token + challenge Bearer + fallback Basic, RBAC par action/repo sur /v2, **flip V2_AUTH_ENABLED=true (breaking)**, V2_ANONYMOUS_PULL ; e2e docker login/push/pull vérifié |
| P1.5 — Audit log | #7 | ✅ fait | `3b44f98` | internal/audit : mutations admin + push/delete-manifest v2 (acteur du Principal, anonymes inclus), hooks login/logout/password, GET /api/admin/audit filtrable, table dans SettingsTab |
| P1.6 — Rate limiting + CORS | #8 | ✅ fait | `058fbc5` | limiteur strict login+/v2/token (RATE_LIMIT_LOGIN_PER_MIN=10), plafond global par IP (RATE_LIMIT_GLOBAL_RPS=100), CORS off par défaut (CORS_ALLOWED_ORIGINS) |
| P1.7 — TLS natif | #9 | ✅ fait | `8eeedbb` | TLS_MODE=off/static/self-signed/acme (autocert TLS-ALPN, pas lego), cert self-signed persisté+réutilisé, Helm tls.* (secret monté, probes HTTPS) — `helm template` à valider côté user |
| P1.8 — UI users + sessions | #10 | ✅ fait | `dba189a` | UsersTab (CRUD, rôle inline, patterns, création), sessions avec revoke + « this session », onglet Users admin-only dans la sidebar (rôle décodé du JWT) |
| P1.9 — GC dry-run | #11 | ✅ fait | `84e87c6` | ?dryRun=true sur POST /gc (mark sans sweep), bouton Preview GC dans StorageTab, test préview==réel |
| P1.10 — Tests intégration P1 | #12 | ✅ fait | `59cfdc2` | flow docker complet via la vraie stack (401 challenge → token → push → pull), RBAC reader via API réelle, révocation de session → refresh mort |
| P2.0 — S3 multipart | #13 | ✅ fait | `3ca3b08` | uploads en parts (uploads/<uuid>/parts/<n>), commit streamé O(16MiB) avec vérif digest (avant : aucune !), PutBlob vérifie aussi, Stats ne compte que blobs/ ; contrat + docker push e2e validés sur MinIO réel |
| P2.1 — Mode mirror | #14 | ✅ fait | `5cca466` | REGISTRY_MODE=mirror (internal/v2/mirror.go) : hit local, miss→fetch upstream write-through, TTL tags (MIRROR_TAG_TTL), stale si upstream down, push direct OK, hits/misses dans /health, events SSE au cache-fill |
| P2.2 — Mirror auth upstream | #15 | ✅ fait | `cef9b6b` | token dance Bearer dans registry.Client (401 challenge → realm → token, cache par scope), Basic conservé ; e2e réel : docker pull alpine via mirror devant registry-1.docker.io |
| P2.3 — Mirror hit/miss | #16 | ✅ fait | `3e12fd0` | compteurs déjà dans /health (P2.1) ; cartes Cache hits/misses + upstream dans StorageTab (choix : pas de nouvel endpoint admin, /health suffit) |
| P2.4 — Tests mirror | #17 | ✅ fait | `c056924` | couverts par mirror_test.go + client_test.go, ajout du scénario multi-arch enfant-par-digest |
| P3.1 — /metrics Prometheus | #18 | ✅ fait | (ce commit) | internal/metrics (registre par défaut, sources swappables anti-double-register), HTTP par route normalisée (garde anti-cardinalité testée), jauges storage, compteurs GC (scheduler+admin), hits/misses mirror, échecs auth ; METRICS_ENABLED=true par défaut |
| P3.2 — /health enrichi | #19 | ✅ fait | (ce commit) | probe storage (latence, degraded), stats cachées 30 s (les jauges Prometheus ne full-listent plus S3 à chaque scrape), free_bytes disque en local (win+unix) |
| P3.3 — Dashboard insights | #20 | ⬜ à faire | | après P3.1/P3.2 |
| P3.4 — OpenTelemetry | #21 | ⬜ à faire | | optionnel |
| P3.5 — Helm ServiceMonitor | #22 | ✅ fait | (ce commit) | serviceaccount.yaml (create/name/annotations) + servicemonitor.yaml (gated metrics.serviceMonitor.enabled, scheme https si tls) — `helm template` à valider côté user |
| P4.1 — Pull tracking | #23 | ⬜ à faire | | |
| P4.2 — Moteur rétention | #24 | ⬜ à faire | | après P4.1 + P1.9 |
| P4.3 — UI rétention | #25 | ⬜ à faire | | |
| P4.4 — Webhooks | #26 | ⬜ à faire | | après P1.5 (acteur dans payload) |
| P4.5 — UI webhooks | #27 | ⬜ à faire | | |
| P4.6 — Tests P4 | #28 | ⬜ à faire | | |
| P5.1 — OpenAPI spec | #29 | ⬜ à faire | | volontairement tardif (API stabilisée après P1–P4) |
| P5.2 — Client TS généré | #30 | ⬜ à faire | | |
| P5.3 — Export/import OCI | #31 | ⬜ à faire | | |
| P5.4 — dockyard-cli | #32 | ⬜ à faire | | |
| P5.5 — Tests P5 | #33 | ⬜ à faire | | |
| P6.1 — Scan Trivy | #34 | ⬜ à faire | | Trivy server mode |
| P6.2 — UI scan | #35 | ⬜ à faire | | |
| P6.3 — Signatures cosign | #36 | ⬜ à faire | | inclut referrers si besoin |
| P6.4 — Tests P6 | #37 | ⬜ à faire | | |
| P7.1 — Diff de tags | #38 | ⬜ à faire | | |
| P7.2 — Recherche serveur | #39 | ⬜ à faire | | |
| P7.3 — Notifications in-app | #40 | ⬜ à faire | | |
| P7.4 — i18n FR/EN | #41 | ⬜ à faire | | après P7.1/P7.2 |
| P7.5 — Helm HPA/PDB | #42 | ⬜ à faire | | HPA gated backend S3 |
| P7.6 — Terraform + Artifact Hub | #43 | ⬜ à faire | | |
| P7.7 — Réplication | #44 | ⬜ à faire | | après P4.4 + P1.4 |
| P7.8 — Quotas | #45 | ⬜ à faire | | après P1.2 + P3.2 |
| P7.9 — Hardening (fuzz/load) | #46 | ⬜ à faire | | |
| P7.10 — zstd LayerBrowser | #47 | ✅ fait | (ce commit) | klauspost/compress/zstd dans parseLayerEntries, test des 3 formats (plain/gzip/zstd) |

## Prochaine étape

**🎉 Phases 1 et 2 COMPLÈTES** (issues #1–#17 fermées). Suite : **Phase 3 — P3.1 /metrics Prometheus** (issue #18) : prometheus/client_golang + middleware echo, métriques HTTP (route/statut/durée), registry (blobs/repos/GC/uploads), hit/miss mirror, échecs auth ; gated METRICS_ENABLED. Astuce MinIO locale : `docker run --rm -d -p 19000:9000 -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin minio/minio server /data` + `="false"` (le .env force true).

## Notes de reprise

- Décisions d'architecture détaillées : plan `~/.claude/plans/dockyard-roadmap-sorted-moore.md` + corps des issues GitHub.
- Le harness de tests : `storagetest.RunBackendContract` tourne sur S3 réel via `DOCKYARD_TEST_S3_ENDPOINT/ACCESS_KEY/SECRET_KEY`.
- Credentials dev locaux dans `.env` (admin/changeme123).
- Vérif e2e locale : builder `dockyard-test.exe`, `REGISTRY_STORAGE_PATH` vers un dossier temp, `PORT=18099`.
- La machine n'a pas accès au cluster k8s de la registry — demander à l'utilisateur pour les vérifs Helm.
