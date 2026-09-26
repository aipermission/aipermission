import { render } from "@testing-library/react";
import { expect, it } from "vitest";
import { DockerConnectorRowActionsTemplate } from "../docker/list-item";
import { DockerConnectorOperationsTemplate } from "../docker/operations";
import { KubernetesConnectorRowActionsTemplate } from "../kubernetes/list-item";
import { KubernetesConnectorOperationsTemplate } from "../kubernetes/operations";
import { RedisConnectorRowActionsTemplate } from "../redis/list-item";
import { RedisConnectorOperationsTemplate } from "../redis/operations";
import { RabbitMQConnectorRowActionsTemplate } from "../rabbitmq/list-item";
import { RabbitMQConnectorOperationsTemplate } from "../rabbitmq/operations";
import { S3ConnectorRowActionsTemplate } from "../s3/list-item";
import { S3ConnectorOperationsTemplate } from "../s3/operations";
import { ClickHouseConnectorRowActionsTemplate } from "../clickhouse/list-item";

it("leaves generic CRUD controls to the host when connectors have no extra row operations", () => {
  const { container } = render(
    <>
      <DockerConnectorRowActionsTemplate />
      <DockerConnectorOperationsTemplate />
      <KubernetesConnectorRowActionsTemplate />
      <KubernetesConnectorOperationsTemplate />
      <RedisConnectorRowActionsTemplate />
      <RedisConnectorOperationsTemplate />
      <RabbitMQConnectorRowActionsTemplate />
      <RabbitMQConnectorOperationsTemplate />
      <S3ConnectorRowActionsTemplate />
      <S3ConnectorOperationsTemplate />
      <ClickHouseConnectorRowActionsTemplate />
    </>,
  );
  expect(container).toBeEmptyDOMElement();
});
