#:package Aspire.Hosting.Docker@13.2.0
#:sdk Aspire.AppHost.Sdk@13.2.0
#:package Aspire.Hosting.PostgreSQL@13.2.0
#:package CommunityToolkit.Aspire.Hosting.Golang@13.2.1-beta.532

using Aspire.Hosting.Pipelines;
using Microsoft.Extensions.Logging;

#pragma warning disable ASPIREPIPELINES001
#pragma warning disable ASPIRECOMPUTE003 // Container registry API is experimental in current Aspire releases.

var builder = DistributedApplication.CreateBuilder(args);

builder.AddDockerComposeEnvironment("env");

var registry = builder.AddContainerRegistry("registry", "registry.sussman.win", "traktv-tg-bot");

var dbUser = builder.AddParameter("postgres-user");
var dbPassword = builder.AddParameter("postgres-password", secret: true);
var dbName = "trakt";

var telegramBotToken = builder.AddParameter("telegram-bot-token", secret: true);
var traktClientId = builder.AddParameter("trakt-client-id");
var traktClientSecret = builder.AddParameter("trakt-client-secret", secret: true);
var telegramChatId = builder.AddParameter("telegram-chat-id");

var postgres = builder.AddPostgres("postgres", dbUser, dbPassword)
	.WithImageTag("17-alpine")
	.WithEnvironment("POSTGRES_DB", dbName)
	.WithDataVolume();

var database = postgres.AddDatabase("traktdb", databaseName: dbName);

var bot = builder.AddGolangApp("bot", ".", "./cmd/bot")
	.WithReference(database)
	.WithEnvironment("TELEGRAM_BOT_TOKEN", telegramBotToken)
	.WithEnvironment("TRAKT_CLIENT_ID", traktClientId)
	.WithEnvironment("TRAKT_CLIENT_SECRET", traktClientSecret)
	.WithEnvironment("TELEGRAM_CHAT_ID", telegramChatId)
	.WithEnvironment("DATABASE_URL", database.Resource.GetConnectionProperty("URI"))
	.WithContainerRegistry(registry)
	.WaitFor(postgres);

builder.Pipeline.AddStep(
	"push-and-prepare-env",
	context =>
	{
		// This step is a dependency barrier so push and prepare-env share one pipeline execution.
		context.Logger.LogInformation("push-and-prepare-env completed.");
		return Task.CompletedTask;
	},
	dependsOn: new[] { "push", "prepare-env" });

builder.Build().Run();

