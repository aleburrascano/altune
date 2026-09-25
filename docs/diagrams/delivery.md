# Delivery

## Backend deploy

From commit to live: PR gate, staging, human approval, blue-green swap. Red boxes are the automatic fallbacks.

```mermaid
flowchart TD
    commit["git commit<br/>husky: block secrets · commitlint"] --> pr["Pull request to main"]
    pr --> gate["Required checks<br/>gate: backend · mobile · overseer tests + module-cycle check<br/>review: status posted by the review skill"]
    gate -->|green| mq["Merge queue<br/>re-runs every suite"]
    mq --> main(["main"])
    main --> changed{"go-api / overseer<br/>code changed?"}
    changed -->|no| skip(["no deploy"])
    changed -->|yes| retest["Re-test the exact commit<br/>test-backend · test-overseer"]

    subgraph staging [Staging · same VM, own compose project + Supabase]
        st_deploy["SSH: git reset --hard origin/main<br/>staging.sh: migrate · compose up --build in place"]
        st_health["poll public /health up to 180s"]
        smoke["smoke.sh<br/>/health · /overseer · overseer buckets_ok"]
        st_deploy --> st_health --> smoke
    end

    retest --> st_deploy
    smoke --> approve{{"approval job<br/>human reviewer on the production environment"}}

    subgraph live_env [Production · deploy job never cancelled mid-run]
        migrate["migrations via psql to Supabase<br/>forward only, before the swap"]
        build["build + start idle color<br/>blue ↔ green"]
        healthy{"idle color healthy<br/>within 180s?"}
        flip["write Caddy upstream.conf · caddy reload"]
        verify{"public /health ok?"}
        drain["drain 20s · stop old color<br/>kept for rollback.sh"]
        overseer["overseer.sh<br/>rebuild in place · 22s log watch"]
        prune["docker image prune"]
        migrate --> build --> healthy
        healthy -->|yes| flip --> verify
        verify -->|yes| drain --> overseer --> prune
    end

    approve --> migrate
    healthy -->|no| keep(["stop idle · live color keeps serving"])
    verify -->|no| restore(["restore old upstream · stop new color"])
    prune --> done(["live"])

    classDef fail fill:#fde2e2,stroke:#c0392b,color:#000
    class keep,restore fail
```

## Mobile release

How an iOS build reaches phones, and what can change without a release.

```mermaid
flowchart LR
    subgraph ios [iOS release · tag driven]
        tag(["git tag v*"]) --> verify["release-ios.yml<br/>runs test-mobile first"]
        verify --> build["macOS runner<br/>expo prebuild · pod install<br/>xcodebuild archive, unsigned"]
        build --> ipa[/"unsigned .ipa"/]
        ipa --> release["GitHub Release"]
        release --> source["update-altstore-source.mjs<br/>push apps.json to altstore branch"]
    end

    source --> altstore(["AltStore on the phone<br/>installs / updates the app"])
    release --> altstore

    subgraph remote [Remote levers · no release needed]
        ks["kill-switches.json on main<br/>sse · telemetry · offline_downloads<br/>detail_enrichment · discovery"]
    end

    ks -->|raw.githubusercontent.com<br/>polled every 5 min + on foreground| app(["Installed app"])
    altstore --> app

    eas["eas.json profiles<br/>Android APK"] -.->|manual, no workflow| app

    note["No OTA updates: any JS change needs a new .ipa"]
    note ~~~ app
```
