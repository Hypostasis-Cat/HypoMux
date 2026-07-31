import { Toaster, type ToasterProps } from "@fluentui/react-components";

const windowSafeOffset = { horizontal: 20, vertical: 60 };

export function AppToaster(props: ToasterProps) {
  return <Toaster {...props} offset={props.offset ?? windowSafeOffset} />;
}
