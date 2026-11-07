---
description: "Rules for the templates that generate cluster repositories: placeholders, Helm values and Flux Kustomizations."
applyTo: "embed/**"
paths:
  - "embed/**"
---

# Template Rules (embed/**)

- For template changes, check both the source templates and the resulting
  generated configuration.
- Preserve placeholders such as `DOTREPLACE...`, `REPLACEWITHCLUSTERNAME`, and
  `${...}`. Verify where substitution occurs before changing generation order.
- Validate Helm values against the relevant chart version and schema.
- Check shared Flux defaults before adding per-resource overrides. Keep CRDs
  and their instances in appropriately ordered Kustomizations.
