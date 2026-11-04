import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { checkoutFetch, underwritingFetch } from './client';
import type {
  AuthorizeResponse,
  CreditPosition,
  Customer,
  Merchant,
  OrderDetail,
  RefundResponse,
} from './types';

export interface AuthorizeVariables {
  customerId: number;
  merchantId: number;
  principalAmount: number;
  installmentCount: number;
  intervalDays: number;
}

export function useAuthorize() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: AuthorizeVariables) =>
      checkoutFetch<AuthorizeResponse>('/api/v1/checkout/authorize', {
        method: 'POST',
        body: JSON.stringify({
          customer_id: vars.customerId,
          merchant_id: vars.merchantId,
          principal_amount: vars.principalAmount,
          installment_count: vars.installmentCount,
          interval_days: vars.intervalDays,
        }),
      }),
    onSuccess: (data) => {
      queryClient.setQueryData(['order', data.order.order_ref], {
        order: data.order,
        installments: data.installments,
      } satisfies OrderDetail);
    },
  });
}

export function useOrder(orderRef: string | null) {
  return useQuery({
    queryKey: ['order', orderRef],
    enabled: orderRef !== null && orderRef !== '',
    queryFn: () =>
      checkoutFetch<OrderDetail>(
        `/api/v1/orders/${encodeURIComponent(orderRef ?? '')}`,
      ),
  });
}

export interface RefundVariables {
  orderRef: string;
  amount: number;
  reason: string;
}

export function useRefund() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: RefundVariables) =>
      checkoutFetch<RefundResponse>(
        `/api/v1/orders/${encodeURIComponent(vars.orderRef)}/refunds`,
        {
          method: 'POST',
          body: JSON.stringify({ amount: vars.amount, reason: vars.reason }),
        },
      ),
    onSuccess: (_data, vars) => {
      void queryClient.invalidateQueries({ queryKey: ['order', vars.orderRef] });
    },
  });
}

export interface CreateCustomerVariables {
  externalRef: string;
  fullName: string;
  creditLimit: number;
}

export function useCreateCustomer() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: CreateCustomerVariables) =>
      underwritingFetch<Customer>('/api/v1/customers', {
        method: 'POST',
        body: JSON.stringify({
          external_ref: vars.externalRef,
          full_name: vars.fullName,
          credit_limit: vars.creditLimit,
        }),
      }),
    onSuccess: (_data, vars) => {
      void queryClient.invalidateQueries({ queryKey: ['credit', vars.externalRef] });
    },
  });
}

export interface CreateMerchantVariables {
  merchantRef: string;
  name: string;
  feeBps: number;
}

export function useCreateMerchant() {
  return useMutation({
    mutationFn: (vars: CreateMerchantVariables) =>
      underwritingFetch<Merchant>('/api/v1/merchants', {
        method: 'POST',
        body: JSON.stringify({
          merchant_ref: vars.merchantRef,
          name: vars.name,
          fee_bps: vars.feeBps,
        }),
      }),
  });
}

export function useCreditPosition(externalRef: string | null) {
  return useQuery({
    queryKey: ['credit', externalRef],
    enabled: externalRef !== null && externalRef !== '',
    queryFn: () =>
      underwritingFetch<CreditPosition>(
        `/api/v1/customers/${encodeURIComponent(externalRef ?? '')}/credit`,
      ),
  });
}
