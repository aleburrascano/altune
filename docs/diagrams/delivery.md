# Delivery

## Backend deploy

From commit to live: PR precheck, merge, then a deploy someone starts by hand. Nothing deploys on merge. Red boxes are the automatic fallbacks.

```mermaid
flowchart TD
    commit["git commit<br/>husky: block secrets · lint changed files"] --> pr["Pull request to main"]
    pr --> precheck["precheck.yml · pr-shape job"]

    subgraph gate [PR gate · precheck.yml]
        shape["scripts/ci/pr-shape.sh"]
    end

    subgraph ghgate [PR gate · GitHub pr-checks]
        ghtests["pr-checks · backend-tests<br/>migrations · INTEGRATION=1 go test · coverage ratchet"]
        ghprecheck["pr-checks · precheck<br/>scripts/precheck.sh origin/main"]
    end

    precheck --> shape
    pr --> ghtests
    pr --> ghprecheck
    ghtests -->|green| main
    ghprecheck -->|green| main
    shape -->|green| main(["merge to main<br/>no merge queue"])
    main --> ci["ci.yml on push to main<br/>backend · overseer · mobile jobs for the paths that changed"]

    dispatch(["deploy.yml · workflow_dispatch only<br/>run by hand on a main commit"])

    subgraph staging [Staging · same VM, own compose project + Supabase]
        st_release["release.sh staging sha<br/>git reset --hard sha · staging.sh"]
        st_up["migrate · compose up --build<br/>/health within 180s"]
        drill["rollback drill<br/>previous build boots on the new schema"]
        smoke["smoke.sh staging<br/>/health version = sha · /overseer · buckets_ok · journey-check"]
        st_release --> st_up --> drill --> smoke
    end

    dispatch --> st_release

    subgraph live_env [Production · release.sh prod sha]
        migrate["prod-migrate.sh<br/>psql to Supabase, forward only, before the swap"]
        build["blue-green.sh<br/>build + start idle color"]
        healthy{"idle color healthy<br/>within 180s?"}
        flip["write Caddy upstream · caddy reload"]
        verify{"public /health ok?"}
        drain["drain 20s · stop old color"]
        overseer["overseer.sh<br/>rebuild in place · 22s log watch"]
        psmoke["smoke.sh prod<br/>on the active color"]
        migrate --> build --> healthy
        healthy -->|yes| flip --> verify
        verify -->|yes| drain --> overseer --> psmoke
    end

    smoke --> migrate
    psmoke --> version{"verify prod version<br/>/health version = sha?"}
    version -->|yes| done(["live"])

    st_fail(["staging step fails · job stops<br/>prod untouched"])
    smoke -.->|any staging step fails| st_fail
    healthy -->|no| keep(["stop idle · live color keeps serving"])
    verify -->|no| restore(["restore old upstream · stop new color"])
    rollback(["rollback.sh<br/>flip back to the previous color"])
    psmoke -.->|any release prod failure| rollback
    version -->|no| vfail(["deploy job fails"])

    classDef fail fill:#fde2e2,stroke:#c0392b,color:#000
    class keep,restore,rollback,st_fail,vfail fail
```

## Mobile release

How an iOS build reaches phones, and what can change without a new build.

```mermaid
flowchart LR
    subgraph ios [iOS release · tag driven]
        tag(["git tag v*"]) --> verify["release-ios.yml<br/>tag a main commit the Gitea gate passed"]
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

    subgraph ota [Over-the-air update · release-ota.yml, manual]
        export["expo export ios<br/>sign the manifest with the key on the runner host"] --> otaship["ota-release.sh on the VM<br/>~/altune-web/updates/prod"]
    end

    otaship -->|Caddy /updates/manifest<br/>checked on each app launch| app

    eas["eas.json profiles<br/>Android APK"] -.->|manual, no workflow| app

    note["A JS-only change ships over the air.<br/>A native change bumps the fingerprint runtime and needs a new .ipa"]
    note ~~~ app
```
