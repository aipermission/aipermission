import type { ReactNode } from "react";
import type { ConnectorFamilyProps, RegisteredConnectorFamily } from "./connector-family-types";

export function ConnectorFamilyProviders({
  families,
  children,
  ...props
}: ConnectorFamilyProps & { families: readonly RegisteredConnectorFamily[] }) {
  return (
    <>
      {families.reduceRight<ReactNode>((content, family) => {
        const Provider = family.Provider;
        return (
          <Provider key={family.kind} {...props}>
            {content}
          </Provider>
        );
      }, children)}
    </>
  );
}
