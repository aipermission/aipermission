import { Database, Plus } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Field, Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { targetEndpoint } from "./model-helpers";
import { KafkaSASLFields } from "./sasl-fields";

export function KafkaCredentialFormTemplate({ targets, form, formMode = "create", state, onChange, onSubmit }) {
  const kafkaTargets = targets.filter((target) => target.connector_kind === "kafka");
  const editing = formMode === "edit";
  return (
    <form className="grid gap-4" onSubmit={onSubmit}>
      {kafkaTargets.length === 0 ? (
        <Notice tone="warn">Create a Kafka / Redpanda connector before adding a credential profile.</Notice>
      ) : null}
      <Field>
        Connector target
        <Select
          value={form.target_id}
          onChange={(event) => onChange({ ...form, target_id: event.target.value })}
          disabled={editing}
          required
        >
          <option value="" disabled>
            Select Kafka / Redpanda target
          </option>
          {kafkaTargets.map((target) => (
            <option value={target.id} key={target.id}>
              {target.name} · {targetEndpoint(target)}
            </option>
          ))}
        </Select>
      </Field>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field>
          Profile label
          <Input value={form.profile_label} onChange={(event) => onChange({ ...form, profile_label: event.target.value })} required />
        </Field>
        <Field>
          Risk label
          <Input value={form.risk_label} onChange={(event) => onChange({ ...form, risk_label: event.target.value })} />
        </Field>
      </div>
      <KafkaSASLFields form={form} editing={editing} onChange={(name, value) => onChange({ ...form, [name]: value })} />
      {form.sasl_mechanism === "plain" ? (
        <Notice tone="warn">The target must use TLS or explicitly allow insecure PLAIN on a trusted private network.</Notice>
      ) : null}
      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      <Button type="submit" disabled={state.state === "saving" || kafkaTargets.length === 0}>
        <Plus className="h-4 w-4" />
        {state.state === "saving" ? "Saving..." : editing ? "Save Kafka credential" : "Create Kafka credential"}
      </Button>
      <Notice>
        <Database className="mr-2 inline h-4 w-4" />
        SASL passwords stay in the encrypted local database and are never returned in connector lists.
      </Notice>
    </form>
  );
}
