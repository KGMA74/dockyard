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
| P3.1 — /metrics Prometheus | #18 | ✅ fait | `048b695` | internal/metrics (registre par défaut, sources swappables anti-double-register), HTTP par route normalisée (garde anti-cardinalité testée), jauges storage, compteurs GC (scheduler+admin), hits/misses mirror, échecs auth ; METRICS_ENABLED=true par défaut |
| P3.2 — /health enrichi | #19 | ✅ fait | `5d16f5c` | probe storage (latence, degraded), stats cachées 30 s (les jauges Prometheus ne full-listent plus S3 à chaque scrape), free_bytes disque en local (win+unix) |
| P3.3 — Dashboard insights | #20 | ✅ fait | `567d13f` | migration 0005 stats_history (échantillon 6 h, purge 90 j), GET /api/admin/insights (historique + top repos par taille avec dédup de digests), InsightsSection dans StorageTab (barres top repos + table de croissance) |
| P3.4 — OpenTelemetry | #21 | ⬜ à faire | | optionnel |
| P3.5 — Helm ServiceMonitor | #22 | ✅ fait | `2707507` | serviceaccount.yaml (create/name/annotations) + servicemonitor.yaml (gated metrics.serviceMonitor.enabled, scheme https si tls) — `helm template` à valider côté user |
| P4.1 — Pull tracking | #23 | ✅ fait | `167e77d` | migration 0002 last_pulls (repo, reference, last_pulled_at, pull_count), PullTracker async (batch 3 s/256, drop si saturé), hook OnPull sur GET manifest (embedded + mirror) |
| P4.2 — Moteur rétention | #24 | ✅ fait | `49b4ef2` | internal/retention : keep-N, unpulled_days (pulls > push), keep_patterns globs, protected_tags, garde digest partagé (skip + raison), CRUD + /retention/run?dryRun, planifié avant le GC quotidien |
| P4.3 — UI rétention | #25 | ✅ fait | `796392e` | RetentionSection dans StorageTab : liste/création/suppression de politiques, Preview plan (table delete/skipped avec raisons), Apply now ; e2e vérifié (keep_n=1 sur 3 tags → 2 supprimés). Fix largeur : les onglets Settings/Storage/Users occupent tout l'espace (max-w-3xl retiré) |
| P4.4 — Webhooks | #26 | ✅ fait | `14c1e5a` | internal/webhooks : outbox SQLite (migration 0004), dispatcher retry backoff expo 30s→32min cap 8 tentatives, HMAC X-Dockyard-Signature, formats generic/slack/discord, events push/delete/retention (+Actor dans events.Event), CRUD + /test synchrone |
| P4.5 — UI webhooks | #27 | ✅ fait | `a94e2ef` | WebhooksSection dans SettingsTab (admin-only) : création (url/secret/format/événements cochables), liste, suppression, bouton test synchrone |
| P4.6 — Tests P4 | #28 | ✅ fait | `84c56d0` | edge cases globs semver ajoutés (zoo de tags réaliste), retry webhooks déjà couvert |
| P5.1 — OpenAPI spec | #29 | ✅ fait | `ca21f2a` | api/openapi.yaml écrit à la main (auth/users/sessions/repos/storage/retention/webhooks/audit/insights/health/v2-token), validé par redocly lint en CI (job ui) |
| P5.2 — Client TS généré | #30 | ✅ fait | `8d47245` | openapi-typescript → ui/src/generated/api.d.ts (npm run gen-api), garde anti-drift en CI (git diff --exit-code), adoption progressive démarrée (type Role) |
| P5.3 — Export/import OCI | #31 | ✅ fait | `10ef9e1` | internal/export : tar OCI image-layout streamé (dédup blobs, multi-arch récursif), import avec buffering des petits blobs seulement (layers streamés + hash-vérifiés), endpoints admin, spec OpenAPI + types régénérés, round-trip digests testé |
| P5.4 — dockyard-cli | #32 | ✅ fait | `5b0114b` | cmd/dockyard-cli (Cobra) : login/repos/tags/delete/gc --dry-run/export/import/users/sessions, refresh silencieux, binaires multi-plateformes attachés aux releases ; e2e complet (backend local !) ; bonus : Preflight export (plus de tar tronqué) |
| P5.5 — Tests P5 | #33 | ✅ fait | (ce commit) | round-trip export/import déjà testé (digests préservés) + tests CLI (config, erreur sans login, refresh silencieux sur 401 avec rotation) |
| P6.1 — Scan Trivy | #34 | ✅ fait | (ce commit) | internal/scan : Dockyard shell le binaire `trivy` embarqué (mode --server) contre un trivy server externe géré par l'opérateur (aucune dépendance Go trivy, pas de CGO) ; table `scans` (migration 0006), dispatcher single-flight avec dédoublonnage par digest, `POST/GET /api/admin/scans[/:id[/report]]`, event `scan` (SSE + webhooks), audit explicite, config `SCAN_*`/`TRIVY_*`, Dockerfile (binaire trivy copié, testé sur `scratch`), Helm `scan.*` (à valider par l'utilisateur, pas d'accès cluster) |
| P6.2 — UI scan | #35 | ✅ fait | (ce commit) | bouton « Scan for vulnerabilities » + badges statut/sévérité dans ImageDetailsPanel (polling 2s tant que queued/running), ScansSection (historique) dans StorageTab, event `scan` ajouté aux checkboxes webhooks ; testé en navigateur réel (push alpine local, scan déclenché, Queued→Failed via polling, historique visible) |
| P6.3 — Signatures cosign | #36 | ✅ fait | (ce commit) | internal/cosign : vérification serveur uniquement contre clés publiques statiques (pas de keyless/Fulcio — décision de scope), signature toujours côté client (cosign CLI) ; REQUIRE_SIGNED_PUSH + overrides par repo (table signing_policies), enforcement au PUT manifest dans v2/handler.go (exempte push par digest + tags .sig/.att/.sbom, sinon le premier push casserait le flux cosign) ; statut `signed` dans GetManifestDetails (embedded+proxy) ; UI : badge Signed/Unsigned dans ImageDetailsPanel + SigningPoliciesSection dans Settings ; referrers API non implémentée (cosign fallback automatiquement sur la convention par tag, donc pas nécessaire au fonctionnement) ; Helm signing.* (secret de clés publiques monté, à valider côté user) |
| P6.4 — Tests P6 | #37 | ✅ fait | (ce commit) | matrice accept/reject cosign étendue (overrides par repo : force on/off, premier match gagne, plusieurs clés dont une seule valide), dédoublonnage du cache de scan vérifié cross-repo (même digest, noms différents → même scan réutilisé), nouveau test du statut `signed` dans GetManifestDetails (absent sans clé, false si non signé, true si signature valide) |
| P7.1 — Diff de tags | #38 | ✅ fait | (ce commit) | `GET /api/admin/repositories/diff?name=&reference_a=&reference_b=` (embedded+proxy) réutilise `parseManifestDetails` des deux côtés, diff par ensemble de digests de layers (pas le JSON brut → un rebuild qui réutilise les mêmes layers de base ressort « inchangé »), delta de taille ; UI : cases à cocher sur les tags dans RepoList (max 2), bouton Compare → `TagDiff.tsx` (taille/arch/OS/signed côte à côte + layers exclusifs par tag) ; testé en navigateur réel (alpine:3.19 vs 3.20, delta +210 537 octets, 1 layer exclusif de chaque côté) |
| P7.2 — Recherche serveur | #39 | ✅ fait | (ce commit) | `GET /api/admin/repositories/search?q=&signed=&limit=&offset=` (embedded+proxy) : correspond sur nom de repo OU tag, filtre `signed` résolu seulement sur les résultats matchés (pas tout le registre), infos scan (statut+critical/high) jointes par digest, tags cosign (.sig/.att/.sbom) exclus des résultats, pagination triée nom+tag ; nouveau champ `db *store.Store` sur `admin.Handler`/`RemoteHandler` (accès aux scans) ; UI : toggle Cards/Dense dans Dashboard, `DenseRepoView.tsx` (table plate paginée, filtre Signed, ouverture du panneau détails), réutilise la barre de recherche existante ; testé en navigateur réel (recherche par nom et par tag, ouverture détails depuis la vue dense) |
| P7.3 — Notifications in-app | #40 | ✅ fait | (ce commit) | événements `gc` et `import` ajoutés (n'existaient pas avant : GC manuel/planifié et import n'émettaient rien) ; GC planifié ne publie que si des blobs ont été supprimés (évite le bruit quotidien), GC manuel publie toujours (feedback de l'action déclenchée) ; `subscribeToPushEvents` généralisé en `subscribeToEvents`/`RegistryEvent` (tous types) ; UI : cloche dans Sidebar (Popover radix-ui, badge non-lus, historique 20 derniers événements en mémoire de session), toasts sonner pour tous les types ; testé en navigateur réel (GC + push vus en direct) |
| P7.4 — i18n FR/EN | #41 | ⬜ à faire | | après P7.1/P7.2 |
| P7.5 — Helm HPA/PDB | #42 | ⬜ à faire | | HPA gated backend S3 |
| P7.6 — Terraform + Artifact Hub | #43 | ⬜ à faire | | |
| P7.7 — Réplication | #44 | ⬜ à faire | | après P4.4 + P1.4 |
| P7.8 — Quotas | #45 | ⬜ à faire | | après P1.2 + P3.2 |
| P7.9 — Hardening (fuzz/load) | #46 | ⬜ à faire | | |
| P7.10 — zstd LayerBrowser | #47 | ✅ fait | `333ee7a` | klauspost/compress/zstd dans parseLayerEntries, test des 3 formats (plain/gzip/zstd) |

## Prochaine étape

**Phases 1, 2, 4, 5 et 6 complètes** (reste P3.4 OTel, optionnel). P7.1, P7.2 et P7.3 faits. Suite
recommandée : **P7.4 — i18n FR/EN** (#41), ou **P7.5 — Helm HPA/PDB** (#42).

## Notes de reprise

- Décisions d'architecture détaillées : plan `~/.claude/plans/dockyard-roadmap-sorted-moore.md` + corps des issues GitHub.
- Le harness de tests : `storagetest.RunBackendContract` tourne sur S3 réel via `DOCKYARD_TEST_S3_ENDPOINT/ACCESS_KEY/SECRET_KEY`.
- Credentials dev locaux dans `.env` (admin/changeme123).
- Vérif e2e locale : builder `dockyard-test.exe`, `REGISTRY_STORAGE_PATH` vers un dossier temp, `PORT=18099`.
- La machine n'a pas accès au cluster k8s de la registry — demander à l'utilisateur pour les vérifs Helm.
- **Fix P6.1 (post-clôture, ce commit)** : le scan Trivy passe en mode **standalone par défaut**
  (le binaire embarqué gère sa propre base de vulnérabilités, `SCAN_ENABLED=true` seul suffit) —
  `TRIVY_SERVER_URL` reste optionnel pour un trivy server externe mutualisé/air-gapped. Corrige au
  passage un bug latent : `--cache-dir` est maintenant toujours passé à trivy (l'image `scratch`
  n'a pas de `HOME`, donc la résolution du cache par trivy était indéterminée même en mode
  serveur). Voir `internal/scan/trivy.go` (`buildTrivyArgs`) et `internal/server/server.go`
  (calcul de `trivyCacheDir` sous `<storage>/trivy-cache`).
