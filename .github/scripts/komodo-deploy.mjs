// Push the Aspire-generated compose + env into the Komodo Stack, then deploy
// and wait for the result. Requires KOMODO_URL / KOMODO_API_KEY /
// KOMODO_API_SECRET; the key belongs to the `trakt-tg-bot-ci` service user,
// which has Write on this one stack and nothing else.
import { readFileSync } from "node:fs";
import { KomodoClient } from "komodo_client";

const env = (name) => {
  const v = process.env[name];
  if (!v) { console.error(`missing ${name}`); process.exit(1); }
  return v;
};

const komodo = KomodoClient(env("KOMODO_URL"), {
  type: "api-key",
  params: { key: env("KOMODO_API_KEY"), secret: env("KOMODO_API_SECRET") },
});

await komodo.write("UpdateStack", {
  id: "trakt-tg-bot",
  config: {
    file_contents: readFileSync("aspire-output/docker-compose.yaml", "utf8"),
    environment: readFileSync("aspire-output/.env.production", "utf8"),
  },
});

// execute() returns as soon as Komodo *accepts* the deploy; poll the Update
// until it actually finishes, so a failed deploy fails this job.
let update = await komodo.execute("DeployStack", { stack: "trakt-tg-bot" });
for (let i = 0; i < 60 && update.status !== "Complete"; i++) {
  await new Promise((r) => setTimeout(r, 5000));
  update = await komodo.read("GetUpdate", { id: update._id.$oid });
}
if (update.status !== "Complete" || !update.success) {
  console.error("Komodo deploy failed:", JSON.stringify(update.logs ?? update, null, 2));
  process.exit(1);
}
console.log("deployed trakt-tg-bot");
