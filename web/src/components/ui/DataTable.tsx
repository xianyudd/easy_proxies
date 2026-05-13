import type { ReactNode } from 'react'
export function DataTable({ headers, children, empty }: {headers: string[]; children?: ReactNode; empty?: string}) {
  return <div className="table-wrap"><table className="data-table"><thead><tr>{headers.map(h => <th key={h}>{h}</th>)}</tr></thead><tbody>{children || <tr><td colSpan={headers.length} className="empty-cell">{empty || '暂无数据'}</td></tr>}</tbody></table></div>
}
