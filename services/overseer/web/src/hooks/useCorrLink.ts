import { createContext, useContext } from "react";

export type CorrLink = (corrId: string) => void;

export const CorrLinkContext = createContext<CorrLink | undefined>(undefined);

export function useCorrLink(): CorrLink | undefined {
  return useContext(CorrLinkContext);
}
