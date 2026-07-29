import { useOutletContext } from "react-router";

export function useGateway() {
  return useOutletContext();
}
