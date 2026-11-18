export type JsonObject = { readonly [key: string]: JsonValue | undefined };

export type JsonValue = string | number | boolean | null | readonly JsonValue[] | JsonObject;
