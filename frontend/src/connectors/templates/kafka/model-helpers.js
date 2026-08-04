export function credentialPayload(form, existingKind = "sasl") {
  const payload = {
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

export function targetEndpoint(target) {
  return String(target?.config?.bootstrap_brokers || "no brokers")
    .split(/[\s,]+/)
    .filter(Boolean)
    .join(", ");
}
