export type OrderStatus =
  | 'PENDING'
  | 'AUTHORIZED'
  | 'ACTIVE'
  | 'COMPLETED'
  | 'CANCELLED'
  | 'REFUNDED';

export type InstallmentStatus =
  | 'SCHEDULED'
  | 'PROCESSING'
  | 'PAID'
  | 'FAILED'
  | 'CANCELLED';

export interface Customer {
  id: number;
  external_ref: string;
  full_name: string;
  credit_limit: number;
  currency: string;
}

export interface Merchant {
  id: number;
  merchant_ref: string;
  name: string;
  fee_bps: number;
  currency: string;
}

export interface Order {
  id: number;
  order_ref: string;
  customer_id: number;
  merchant_id: number;
  principal_amount: number;
  currency: string;
  installment_count: number;
  fee_amount: number;
  status: OrderStatus;
  created_at: string;
}

export interface Installment {
  id: number;
  number: number;
  amount: number;
  currency: string;
  due_date: string;
  status: InstallmentStatus;
  paid_at?: string | null;
}

export interface AuthorizeResponse {
  order: Order;
  installments: Installment[];
  fee_amount: number;
  merchant_payout: number;
}

export interface OrderDetail {
  order: Order;
  installments: Installment[];
}

export interface CreditPosition {
  customer_id: number;
  external_ref: string;
  credit_limit: number;
  outstanding: number;
  available: number;
  currency: string;
}

export interface RefundResponse {
  order_id: number;
  order_ref: string;
  journal_entry_id: number;
  refunded_amount: number;
  fee_refunded: number;
  merchant_reduction: number;
  order_status: OrderStatus;
  remaining_outstanding: number;
}

export interface ApiErrorBody {
  error: {
    code: string;
    message: string;
  };
}
