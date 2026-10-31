import { apiClient } from './client'
import type { PaginatedResponse } from '@/types'

export type CashStatus = 'pending' | 'paid' | 'rejected'
export interface CashSummary {
  eligible_amount: string
  pending_amount: string
  next_expires_at: string | null
  server_time: string
}
export interface CashReply {
  id: number
  application_id: number
  content: string
  decision: CashStatus
  delivery_status: 'sending' | 'sent' | 'failed' | 'unknown'
  created_at: string
  sent_at: string | null
}
export interface CashApplication {
  id: number
  user_id: number
  username: string
  email: string
  amount: string
  currency: 'USD'
  status: CashStatus
  payout_not_before: string
  created_at: string
  resolved_at: string | null
  replies?: CashReply[]
}
const root = (admin = false) => admin ? '/admin/affiliates/withdrawals' : '/user/aff/withdrawals'

export function formatCashAmount(value: string): string {
  return `USD ${value.replace(/(\.\d*?[1-9])0+$|\.0+$/, '$1')}`
}
export function cashAmountUnits(value: string): bigint | null {
  if (!/^\d{1,10}(\.\d{1,8})?$/.test(value)) return null
  const [whole, fraction = ''] = value.split('.')
  return BigInt(whole) * 100000000n + BigInt(fraction.padEnd(8, '0'))
}
export const affiliateCashAPI = {
  async summary(): Promise<CashSummary> {
    return (await apiClient.get<CashSummary>(`${root()}/summary`)).data
  },
  async apply(email: string, amount: string, file: File, key: string): Promise<CashApplication> {
    const form = new FormData()
    form.set('email', email)
    form.set('amount', amount)
    form.set('payment_qr', file)
    return (await apiClient.post<CashApplication>(root(), form, {
      headers: { 'Content-Type': 'multipart/form-data', 'Idempotency-Key': key }
    })).data
  },
  async list(admin: boolean, page: number, status = ''): Promise<PaginatedResponse<CashApplication>> {
    return (await apiClient.get<PaginatedResponse<CashApplication>>(root(admin), { params: { page, page_size: 10, status } })).data
  },
  async get(id: number, admin: boolean): Promise<CashApplication> {
    return (await apiClient.get<CashApplication>(`${root(admin)}/${id}`)).data
  },
  async qr(id: number, admin: boolean): Promise<Blob> {
    return (await apiClient.get<Blob>(`${root(admin)}/${id}/payment-qr`, { responseType: 'blob' })).data
  },
  async reply(id: number, content: string, decision: CashStatus, key: string): Promise<CashApplication> {
    return (await apiClient.post<CashApplication>(`${root(true)}/${id}/replies`, { content, decision }, {
      headers: { 'Idempotency-Key': key }, timeout: 180000
    })).data
  },
  async retry(id: number, replyID: number, confirmUncertain = false): Promise<CashApplication> {
    return (await apiClient.post<CashApplication>(`${root(true)}/${id}/replies/${replyID}/retry`, { confirm_uncertain: confirmUncertain }, { timeout: 180000 })).data
  }
}
