import { Button, Input, useId } from "@fluentui/react-components";
import { Add20Regular, Delete20Regular } from "@fluentui/react-icons";
import { useEffect, useRef } from "react";
import { MAX_DNS_SERVERS } from "./dnsSettings";

export function SettingsAddressList({ values, label, addLabel, removeLabel, placeholder, emptyHint, errors, disabled, minimum = 0, type = "text", onChange }: {
  values: string[];
  label: string;
  addLabel: string;
  removeLabel: string;
  placeholder: string;
  emptyHint?: string;
  errors: string[];
  disabled: boolean;
  minimum?: number;
  type?: "text" | "url";
  onChange: (values: string[]) => void;
}) {
  const id = useId("dns-address-list");
  const items = useRef<HTMLDivElement>(null);
  const focusAdded = useRef(false);
  useEffect(() => {
    if (focusAdded.current) {
      items.current?.querySelector<HTMLInputElement>(`#${id}-${values.length - 1}`)?.focus();
      focusAdded.current = false;
    }
  }, [id, values.length]);
  return <div className="settings-address-list">
    <div className="settings-address-items" ref={items}>
    {values.length === 0 && <div className="settings-address-empty">{emptyHint}</div>}
    {values.map((value, index) => <div className="settings-address-entry" key={`${id}-${index}`}>
      <div className="settings-address-fields">
        <label className="visually-hidden" htmlFor={`${id}-${index}`}>{label} {index + 1}</label>
        <div className="settings-address-controls">
          <span className="settings-address-number" aria-hidden="true">{index + 1}</span>
          <Input
            id={`${id}-${index}`} name={`${label.toLowerCase()}_server_${index + 1}`}
            type={type} value={value} placeholder={placeholder} disabled={disabled}
            autoComplete="off" spellCheck={false}
            aria-invalid={Boolean(errors[index])} aria-describedby={errors[index] ? `${id}-error-${index}` : undefined}
            onChange={(_, data) => onChange(values.map((current, position) => position === index ? data.value : current))}
          />
          <Button appearance="subtle" icon={<Delete20Regular />} disabled={disabled || values.length <= minimum}
            aria-label={`${removeLabel} ${index + 1}`} title={`${removeLabel} ${index + 1}`}
            onClick={() => onChange(values.filter((_, position) => position !== index))} />
        </div>
        {errors[index] && <span id={`${id}-error-${index}`} className="settings-address-error" aria-live="polite">{errors[index]}</span>}
      </div>
    </div>)}
    </div>
    <div className="settings-address-footer">
    <Button className="settings-address-add" appearance="secondary" icon={<Add20Regular />} disabled={disabled || values.length >= MAX_DNS_SERVERS}
      onClick={() => { focusAdded.current = true; onChange([...values, ""]); }}>{addLabel}</Button>
    <span className="settings-address-count" aria-label={`${label}: ${values.length} / ${MAX_DNS_SERVERS}`}>{values.length} / {MAX_DNS_SERVERS}</span>
    </div>
  </div>;
}
