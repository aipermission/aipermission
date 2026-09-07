import { Send } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Input, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { RoutingKeyPicker } from "./routing-key-picker";

export function RabbitPublishForm({ browser, styles }) {
  const setPublish = (values) => browser.setPublish((current) => ({ ...current, ...values }));
  return (
    <form
      className="grid h-full min-h-0 grid-rows-[auto_auto_auto_minmax(0,1fr)_auto] gap-3"
      onSubmit={(event) => {
        event.preventDefault();
        void browser.publishMessage();
      }}
    >
      <Notice tone="warn">This creates a new RabbitMQ message. With the default exchange, set Routing key to the destination queue name.</Notice>
      <div className="grid gap-2 md:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)]">
        <FieldBlock label="Exchange" help="amq.default is RabbitMQ's default exchange. It routes directly to the queue named by the routing key." mutedClass={styles.muted}>
          <Input className={styles.input} value={browser.publish.exchange} onChange={(event) => setPublish({ exchange: event.target.value })} placeholder="amq.default" aria-label="Publish exchange" />
        </FieldBlock>
        <FieldBlock label="Routing key" help={browser.activeQueue ? `Use ${browser.activeQueue} to publish to the selected queue via amq.default.` : "Usually the queue name when using amq.default."} mutedClass={styles.muted}>
          <div className={`grid gap-2 ${browser.publish.customRoutingKey ? "md:grid-cols-2" : ""}`}>
            <RoutingKeyPicker
              queues={browser.queues}
              value={browser.publish.routingKey}
              custom={browser.publish.customRoutingKey}
              onQueue={(routingKey) => setPublish({ customRoutingKey: false, routingKey })}
              onCustom={() => setPublish({ customRoutingKey: true, routingKey: browser.publish.routingKey.trim() ? browser.publish.routingKey : "" })}
              styles={styles}
            />
            {browser.publish.customRoutingKey ? (
              <Input className={styles.input} value={browser.publish.routingKey} onChange={(event) => setPublish({ routingKey: event.target.value })} placeholder="Custom routing key" aria-label="Custom routing key" />
            ) : null}
          </div>
        </FieldBlock>
      </div>
      <FieldBlock label="Properties JSON" help="Optional AMQP properties. content_type helps consumers parse JSON payloads." mutedClass={styles.muted}>
        <Input className={styles.input} value={browser.publish.properties} onChange={(event) => setPublish({ properties: event.target.value })} placeholder='{"content_type":"application/json"}' aria-label="Publish properties JSON" />
      </FieldBlock>
      <FieldBlock label="Payload" help="Message body to publish. Keep secrets out unless this write was explicitly approved." mutedClass={styles.muted} grow>
        <Textarea className={`h-full min-h-0 resize-none font-mono text-xs ${styles.input}`} value={browser.publish.payload} onChange={(event) => setPublish({ payload: event.target.value })} placeholder='{"type":"test","ok":true}' aria-label="Publish payload" />
      </FieldBlock>
      <div className="flex justify-end">
        <Button type="submit" className="h-9 px-4 text-sm" disabled={browser.state.state !== "idle" || !browser.publish.routingKey.trim() || !browser.publish.payload}>
          <Send className="h-4 w-4" />
          Publish message
        </Button>
      </div>
    </form>
  );
}

function FieldBlock({ label, help, mutedClass, children, grow = false }) {
  return (
    <div className={`grid min-h-0 gap-1 text-sm font-medium ${grow ? "grid-rows-[auto_minmax(0,1fr)_auto]" : ""}`}>
      <span>{label}</span>
      {children}
      {help ? <span className={`text-xs font-normal ${mutedClass}`}>{help}</span> : null}
    </div>
  );
}
