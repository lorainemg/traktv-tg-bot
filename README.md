# traktv-tg-bot

Config parameters:
Country watch, timezone (by country), do you want to delete caught up episodes, 


## CI/CD deployment

This repository includes a GitHub Actions workflow at
`.github/workflows/deploy-main.yml` that runs on every push to `main`.

The workflow:

- runs `aspire do push-and-prepare-env --environment production`
- deploys `aspire-output/docker-compose.yaml` to Portainer at `PORTAINER_URL`
  by updating the configured stack

Required GitHub repository secrets:

- `PORTAINER_URL` (for example `https://portainer.sussman.win`)
- `PORTAINER_API_TOKEN`
- `PORTAINER_ENDPOINT_ID`
- `PORTAINER_STACK_ID`
- `POSTGRES_USER`
- `POSTGRES_PASSWORD`
- `TELEGRAM_BOT_TOKEN`
- `TRAKT_CLIENT_ID`
- `TRAKT_CLIENT_SECRET`
- `TELEGRAM_CHAT_ID`
