import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";
import type { ComponentType } from "react";
import type { ConsoleWorkspaceSlotProps } from "../../components/console/console-workspace-types";
import { gatewayTargetFixture } from "../../test/connector-inventory-fixtures";
import { consoleWorkspaceFixture } from "../../test/console-workspace-fixtures";
import { PostgresConnectorConsoleTemplate } from "./postgres/console";
import { ClickHouseConnectorConsoleTemplate } from "./clickhouse/console";
import { S3ConnectorConsoleTemplate } from "./s3/console";
import { RedisConnectorConsoleTemplate } from "./redis/console";
import { RabbitMQConnectorConsoleTemplate } from "./rabbitmq/console";
import { KafkaConnectorConsoleTemplate } from "./kafka/console";
import { MailConnectorConsoleTemplate } from "./mail/console";

const slots = [
  { kind: "postgres", label: "Postgres", Console: PostgresConnectorConsoleTemplate },
  { kind: "clickhouse", label: "ClickHouse", Console: ClickHouseConnectorConsoleTemplate },
  { kind: "s3", label: "S3", Console: S3ConnectorConsoleTemplate },
  { kind: "redis", label: "Redis", Console: RedisConnectorConsoleTemplate },
  { kind: "rabbitmq", label: "RabbitMQ", Console: RabbitMQConnectorConsoleTemplate },
  { kind: "kafka", label: "Kafka", Console: KafkaConnectorConsoleTemplate },
  { kind: "mail", label: "Mail", Console: MailConnectorConsoleTemplate },
] satisfies { kind: string; label: string; Console: ComponentType<ConsoleWorkspaceSlotProps> }[];

function propsFor(kind: string, session: ConsoleWorkspaceSlotProps["session"]): ConsoleWorkspaceSlotProps {
  return consoleWorkspaceFixture({
    target: gatewayTargetFixture({ connector_kind: kind, ref: `${kind}:3:11`, config: { host: "host.test" } }),
    session,
  });
}

it.each(slots)("$kind binds the generic workspace contract to its native structured console", async ({ kind, label, Console }) => {
  const props = propsFor(kind, { active: false, startedAt: "" });
  render(<Console {...props} />);
  expect(screen.getByText(`No active ${label} session`)).toBeInTheDocument();
  await userEvent.setup().click(screen.getByRole("button"));
  expect(props.onNewStructuredSession).toHaveBeenCalledOnce();
});

it("uses native Valkey product metadata at the generic Redis console boundary", () => {
  const props = propsFor("redis", null);
  props.target.config = { server_family: "valkey", host: "cache.test", port: 6379, database: 0 };
  render(<RedisConnectorConsoleTemplate {...props} />);
  expect(screen.getByText("No active Valkey session")).toBeInTheDocument();
  expect(screen.getByText("Valkey · cache.test:6379 db 0")).toBeInTheDocument();
});

it("uses native Redpanda product metadata at the generic Kafka console boundary", () => {
  const props = propsFor("kafka", null);
  props.target.config = { server_family: "redpanda", bootstrap_brokers: "broker.test:9092" };
  render(<KafkaConnectorConsoleTemplate {...props} />);
  expect(screen.getByText("No active Redpanda session")).toBeInTheDocument();
});

it.each(slots)("$kind rejects live session state at the structured workspace boundary", ({ kind, label, Console }) => {
  render(<Console {...propsFor(kind, { id: 9, status: "live" })} />);
  expect(screen.getByText(`No active ${label} session`)).toBeInTheDocument();
});
