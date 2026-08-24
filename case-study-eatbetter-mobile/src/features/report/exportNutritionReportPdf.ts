import { File } from 'expo-file-system';
import * as Print from 'expo-print';
import * as Sharing from 'expo-sharing';

import type { MealRecord } from '../../domain/meal';
import { buildSevenDayNutritionReport } from './buildSevenDayNutritionReport';
import { renderNutritionReportHtml } from './renderNutritionReportHtml';

export type NutritionReportExportErrorCode =
  | 'sharing_unavailable'
  | 'pdf_generation_failed'
  | 'invalid_generated_pdf'
  | 'sharing_failed';

const ERROR_MESSAGES: Record<NutritionReportExportErrorCode, string> = {
  sharing_unavailable: 'Bu cihazda PDF paylaşımı kullanılamıyor.',
  pdf_generation_failed: 'PDF raporu oluşturulamadı. Lütfen tekrar deneyin.',
  invalid_generated_pdf: 'PDF raporu oluşturulamadı. Lütfen tekrar deneyin.',
  sharing_failed: 'PDF paylaşımı açılamadı. Lütfen tekrar deneyin.',
};

const A4_WIDTH_POINTS = 595.28;
const A4_HEIGHT_POINTS = 841.89;

export class NutritionReportExportError extends Error {
  constructor(public readonly code: NutritionReportExportErrorCode) {
    super(ERROR_MESSAGES[code]);
    this.name = 'NutritionReportExportError';
  }
}

function deleteGeneratedPdfBestEffort(file: File | null): void {
  if (file === null) {
    return;
  }

  try {
    if (file.exists) {
      file.delete();
    }
  } catch {
    // Cleanup is deliberately non-fatal after the share interaction settles.
  }
}

export async function exportSevenDayNutritionReportPdf(
  meals: MealRecord[],
  referenceDate: Date,
  generatedAt: Date,
): Promise<void> {
  let sharingAvailable: boolean;

  try {
    sharingAvailable = await Sharing.isAvailableAsync();
  } catch {
    throw new NutritionReportExportError('sharing_unavailable');
  }

  if (!sharingAvailable) {
    throw new NutritionReportExportError('sharing_unavailable');
  }

  let html: string;

  try {
    const report = buildSevenDayNutritionReport(
      meals,
      referenceDate,
      generatedAt,
    );
    html = renderNutritionReportHtml(report);
  } catch {
    throw new NutritionReportExportError('pdf_generation_failed');
  }

  let generatedPdf: File | null = null;

  try {
    let result: Print.FilePrintResult;

    try {
      result = await Print.printToFileAsync({
        html,
        width: A4_WIDTH_POINTS,
        height: A4_HEIGHT_POINTS,
      });
    } catch {
      throw new NutritionReportExportError('pdf_generation_failed');
    }

    try {
      generatedPdf = new File(result.uri);
    } catch {
      throw new NutritionReportExportError('invalid_generated_pdf');
    }

    if (
      !Number.isInteger(result.numberOfPages) ||
      result.numberOfPages < 1
    ) {
      throw new NutritionReportExportError('invalid_generated_pdf');
    }

    let isValidFile: boolean;

    try {
      const fileSize = generatedPdf.size;

      isValidFile =
        generatedPdf.exists &&
        Number.isFinite(fileSize) &&
        fileSize > 0;
    } catch {
      throw new NutritionReportExportError('invalid_generated_pdf');
    }

    if (!isValidFile) {
      throw new NutritionReportExportError('invalid_generated_pdf');
    }

    try {
      await Sharing.shareAsync(result.uri, {
        mimeType: 'application/pdf',
        UTI: 'com.adobe.pdf',
        dialogTitle: 'Beslenme Raporunu Paylaş',
      });
    } catch {
      throw new NutritionReportExportError('sharing_failed');
    }
  } finally {
    deleteGeneratedPdfBestEffort(generatedPdf);
  }
}