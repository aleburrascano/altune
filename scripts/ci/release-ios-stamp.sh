#!/usr/bin/env bash

set -euo pipefail

node -e "
  const fs=require('fs');
  const j=JSON.parse(fs.readFileSync('app.json','utf8'));
  j.expo.version=process.env.VERSION;
  j.expo.ios=j.expo.ios||{};
  j.expo.ios.buildNumber=String(process.env.BUILD_NUMBER);
  fs.writeFileSync('app.json',JSON.stringify(j,null,2));
  console.log('version',j.expo.version,'build',j.expo.ios.buildNumber);
"
