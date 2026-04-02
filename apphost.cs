#:package Aspire.Hosting.Docker@13.2.0
#:sdk Aspire.AppHost.Sdk@13.2.0
#:package Aspire.Hosting.PostgreSQL@13.2.0
#:package CommunityToolkit.Aspire.Hosting.Golang@13.2.1-beta.532

using Aspire.Hosting.Docker.Resources.ComposeNodes;
using Aspire.Hosting.Docker.Resources.ServiceNodes;
using Aspire.Hosting.Pipelines;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Diagnostics.HealthChecks;
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
var tmdbApiKey = builder.AddParameter("tmdb-api-key");
var env = builder.ExecutionContext.IsRunMode ? "dev" : "prod";

var postgres = builder.AddPostgres("postgres", dbUser, dbPassword)
	.WithImageTag("17-alpine")
	.WithEnvironment("POSTGRES_DB", dbName)
	.WithDataVolume()
	.PublishAsDockerComposeService((resource, service) =>
	{
		service.Healthcheck = new Healthcheck
		{
			Test = ["CMD-SHELL", $"pg_isready -U ${{POSTGRES_USER}} -d {dbName}"],
			Interval = "2s",
			Timeout = "5s",
			Retries = 5,
			StartPeriod = "2s"
		};
		service.Ports.Add("127.0.0.1:5432:5432");
	});

var database = postgres.AddDatabase("traktdb", databaseName: dbName);

var bot = builder.AddDockerfile("bot", ".", "./Dockerfile", env)
	.WithReference(database)
	.WithEnvironment("TELEGRAM_BOT_TOKEN", telegramBotToken)
	.WithEnvironment("TRAKT_CLIENT_ID", traktClientId)
	.WithEnvironment("TRAKT_CLIENT_SECRET", traktClientSecret)
	.WithEnvironment("TMDB_API_KEY", tmdbApiKey)
	.WithEnvironment("ENV", env)
	.WithEnvironment(context =>
	{
		var dbUri = database.Resource.GetConnectionProperty("URI");
		context.EnvironmentVariables["DATABASE_URL"] = ReferenceExpression.Create($"{dbUri}?sslmode=disable");
	})
	.WithContainerRegistry(registry)
	.WaitFor(postgres)
	.PublishAsDockerComposeService((resource, service) =>
	{
		service.DependsOn["postgres"] = new ServiceDependency
		{
			Condition = "service_healthy"
		};
	});

if (env == "dev")
	bot.WithBindMount(".", "/app");

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

