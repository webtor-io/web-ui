---
name: deploy
description: Deploy web-ui to production (webtor.io + webtor.cc) or stage (webtor.cc only). Suggest this skill after git commit and push.
user-invocable: true
allowed-tools: Bash, AskUserQuestion
argument-hint: [stage]
---

Deploy the web-ui project using the helmfile sync script.

## Behavior

1. If the argument is `stage`, deploy only to **webtor.cc** (stage) by running:
   ```
   cd ../infra/helmfile && ./sync.sh --wait web-ui-alt
   ```

2. Otherwise (no argument or `prod` / `all`), deploy to **both webtor.io and webtor.cc** (production) by running:
   ```
   cd ../infra/helmfile && ./sync.sh --wait web
   ```

3. If no argument is provided, ask the user which target they want:
   - **Production** (webtor.io + webtor.cc)
   - **Stage** (webtor.cc only)

4. Stream the output so the user can follow the deployment progress. Use a timeout of 600000ms (10 minutes) for the command.

5. After the command finishes, report success or failure.