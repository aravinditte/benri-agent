export class CapabilityError extends Error {
  code = "CAPABILITY_DENIED";
  constructor(message: string) { super(message); this.name = "CapabilityError"; }
}
export class TenantScopeError extends Error {
  code = "TENANT_SCOPE_VIOLATION";
  constructor(message: string) { super(message); this.name = "TenantScopeError"; }
}
export class NotFoundError extends Error {
  code = "NOT_FOUND";
  constructor(message: string) { super(message); this.name = "NotFoundError"; }
}
