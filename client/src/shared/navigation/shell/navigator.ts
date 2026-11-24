import { useExpoRouter, type Href } from '@shared/navigation/adapters/expoRouterNavigator';

export interface Navigator {
  push(href: Href): void;
  replace(href: Href): void;
  back(): void;
  canGoBack(): boolean;
}

export function useNavigator(): Navigator {
  return useExpoRouter();
}
