#!/usr/bin/env bash

set -u

threads_query=$(cat <<'GRAPHQL'
query($owner:String!,$name:String!,$number:Int!){
    repository(owner:$owner,name:$name){
      pullRequest(number:$number){
        reviewThreads(first:100){ nodes{
          id isResolved
          comments(first:1){ nodes{ author{ login } body } }
        } }
      } } }
GRAPHQL
)
resolve_mutation=$(cat <<'GRAPHQL'
mutation($id:ID!){ resolveReviewThread(input:{threadId:$id}){ thread{ isResolved } } }
GRAPHQL
)

ids=$(gh api graphql \
  -f owner="$OWNER" \
  -f name="$REPO_NAME" \
  -F number="$PR_NUMBER" \
  -f query="$threads_query" \
  --jq '.data.repository.pullRequest.reviewThreads.nodes[]
        | select(.isResolved == false)
        | select(.comments.nodes[0].author.login == "github-actions")
        | select(.comments.nodes[0].body | contains("react-doctor"))
        | .id')
for id in $ids; do
  echo "resolving react-doctor thread $id"
  gh api graphql -f id="$id" \
    -f query="$resolve_mutation" >/dev/null
done
echo "react-doctor threads resolved (advisory)"
