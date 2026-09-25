type KafkaCredentialForm = {
  profile_label: string;
  sasl_mechanism: string;
  existing_sasl_mechanism?: string;
  username?: string;
  password?: string;
  risk_label?: string;
};

export function credentialPayload(form: KafkaCredentialForm, existingKind = "sasl") {
  const payload: {
    kind: string;
    label: string;
    public: { mechanism: string; username: string };
    risk_label: string;
    secret?: { password: string };
  } = {
    kind: existingKind || "sasl",
    label: form.profile_label,
    public: { mechanism: form.sasl_mechanism || "none", username: form.sasl_mechanism === "none" ? "" : form.username || "" },
    risk_label: form.risk_label || "stream read",
  };
  if (form.sasl_mechanism === "none") payload.secret = { password: "" };
  else if (form.password) payload.secret = { password: form.password };
  else if ((form.existing_sasl_mechanism || "none") === "none") throw new Error("Password is required when enabling SASL.");
  return payload;
}

export function targetEndpoint(target: { config?: { bootstrap_brokers?: unknown } } | null | undefined) {
  if (!target?.config?.bootstrap_brokers) return "no brokers";
  return String(target.config.bootstrap_brokers)
    .split(/[\s,]+/)
    .filter(Boolean)
    .join(", ");
}
