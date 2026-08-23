import { ActivityIndicator, Image, Pressable, StyleSheet, Text, View } from 'react-native';

import type { PreparedMealImage } from '../../domain/mealImage';
import { getMealImageInputErrorMessage, type MealImageInputError } from './mealImageErrorPresentation';
import type { MealImageInputOperation } from './useMealImageInput';

type MealImageInputCardProps = {
  image: PreparedMealImage | null;
  operation: MealImageInputOperation;
  error: MealImageInputError | null;
  canManageInput: boolean;
  canInterpret: boolean;
  onTakePhoto: () => Promise<void>;
  onSelectFromGallery: () => Promise<void>;
  onRemove: () => void;
  onInterpret: () => Promise<void>;
};

export function MealImageInputCard({
  image,
  operation,
  error,
  canManageInput,
  canInterpret,
  onTakePhoto,
  onSelectFromGallery,
  onRemove,
  onInterpret,
}: MealImageInputCardProps) {
  const operationInFlight = operation !== 'idle';
  const pickerDisabled = !canManageInput || operationInFlight;

  return (
    <View style={styles.card}>
      <View style={styles.header}>
        <Text style={styles.title}>Öğün fotoğrafı</Text>
        <Text style={styles.description}>
          Yiyecekleri tanımak için tek bir fotoğraf seç veya çek.
        </Text>
      </View>

      {image === null ? (
        <View style={styles.pickerActions}>
          <Pressable
            accessibilityRole="button"
            accessibilityState={{ disabled: pickerDisabled }}
            disabled={pickerDisabled}
            onPress={() => void onTakePhoto()}
            style={({ pressed }) => [
              styles.primaryPickerButton,
              pickerDisabled && styles.buttonDisabled,
              pressed && !pickerDisabled && styles.buttonPressed,
            ]}
          >
            <Text style={styles.primaryPickerButtonText}>Fotoğraf Çek</Text>
          </Pressable>
          <Pressable
            accessibilityRole="button"
            accessibilityState={{ disabled: pickerDisabled }}
            disabled={pickerDisabled}
            onPress={() => void onSelectFromGallery()}
            style={({ pressed }) => [
              styles.secondaryPickerButton,
              pickerDisabled && styles.buttonDisabled,
              pressed && !pickerDisabled && styles.buttonPressed,
            ]}
          >
            <Text style={styles.secondaryPickerButtonText}>Galeriden Seç</Text>
          </Pressable>
        </View>
      ) : (
        <View style={styles.preparedArea}>
          <Image
            accessibilityLabel="Hazırlanan öğün fotoğrafı"
            resizeMode="contain"
            source={{ uri: image.uri }}
            style={[styles.preview, { aspectRatio: image.width / image.height }]}
          />

          <Text style={styles.replaceLabel}>Değiştir</Text>
          <View style={styles.replaceActions}>
            <Pressable
              accessibilityRole="button"
              accessibilityState={{ disabled: pickerDisabled }}
              disabled={pickerDisabled}
              onPress={() => void onTakePhoto()}
              style={({ pressed }) => [
                styles.compactButton,
                pickerDisabled && styles.buttonDisabled,
                pressed && !pickerDisabled && styles.buttonPressed,
              ]}
            >
              <Text style={styles.compactButtonText}>Yenisini Çek</Text>
            </Pressable>
            <Pressable
              accessibilityRole="button"
              accessibilityState={{ disabled: pickerDisabled }}
              disabled={pickerDisabled}
              onPress={() => void onSelectFromGallery()}
              style={({ pressed }) => [
                styles.compactButton,
                pickerDisabled && styles.buttonDisabled,
                pressed && !pickerDisabled && styles.buttonPressed,
              ]}
            >
              <Text style={styles.compactButtonText}>Galeriden Değiştir</Text>
            </Pressable>
            <Pressable
              accessibilityRole="button"
              accessibilityState={{ disabled: pickerDisabled }}
              disabled={pickerDisabled}
              onPress={onRemove}
              style={({ pressed }) => [
                styles.removeButton,
                pickerDisabled && styles.buttonDisabled,
                pressed && !pickerDisabled && styles.buttonPressed,
              ]}
            >
              <Text style={styles.removeButtonText}>Kaldır</Text>
            </Pressable>
          </View>
        </View>
      )}

      {operationInFlight ? (
        <View accessibilityLiveRegion="polite" style={styles.operationStatus}>
          <ActivityIndicator color="#28785f" size="small" />
          <Text style={styles.operationText}>
            {operation === 'preparing' ? 'Fotoğraf hazırlanıyor…' : 'Fotoğraf seçiliyor…'}
          </Text>
        </View>
      ) : null}

      {error !== null ? (
        <Text accessibilityLiveRegion="polite" style={styles.errorText}>
          {getMealImageInputErrorMessage(error)}
        </Text>
      ) : null}

      {image !== null ? (
        <Pressable
          accessibilityRole="button"
          accessibilityState={{ disabled: !canInterpret }}
          disabled={!canInterpret}
          onPress={() => void onInterpret()}
          style={({ pressed }) => [
            styles.interpretButton,
            !canInterpret && styles.interpretButtonDisabled,
            pressed && canInterpret && styles.buttonPressed,
          ]}
        >
          <Text
            style={[
              styles.interpretButtonText,
              !canInterpret && styles.interpretButtonTextDisabled,
            ]}
          >
            Fotoğrafı Yorumla
          </Text>
        </Pressable>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    gap: 14,
    borderWidth: 1,
    borderColor: '#dce7e1',
    borderRadius: 18,
    padding: 18,
    backgroundColor: '#ffffff',
  },
  header: { gap: 5 },
  title: { color: '#1d2b26', fontSize: 16, fontWeight: '700' },
  description: { color: '#64716c', fontSize: 14, lineHeight: 21 },
  pickerActions: { gap: 10 },
  primaryPickerButton: {
    minHeight: 50,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: 13,
    backgroundColor: '#28785f',
  },
  primaryPickerButtonText: { color: '#ffffff', fontSize: 16, fontWeight: '700' },
  secondaryPickerButton: {
    minHeight: 50,
    alignItems: 'center',
    justifyContent: 'center',
    borderWidth: 1,
    borderColor: '#a8c9bb',
    borderRadius: 13,
    backgroundColor: '#f7fbf9',
  },
  secondaryPickerButtonText: { color: '#1f664f', fontSize: 16, fontWeight: '700' },
  preparedArea: { gap: 11 },
  preview: {
    width: '100%',
    maxHeight: 360,
    minHeight: 180,
    borderRadius: 14,
    backgroundColor: '#eef2f0',
  },
  replaceLabel: { color: '#52605b', fontSize: 13, fontWeight: '700' },
  replaceActions: { flexDirection: 'row', flexWrap: 'wrap', gap: 9 },
  compactButton: {
    minHeight: 42,
    justifyContent: 'center',
    borderWidth: 1,
    borderColor: '#cddbd4',
    borderRadius: 10,
    paddingHorizontal: 13,
    backgroundColor: '#ffffff',
  },
  compactButtonText: { color: '#28785f', fontSize: 14, fontWeight: '700' },
  removeButton: {
    minHeight: 42,
    justifyContent: 'center',
    borderWidth: 1,
    borderColor: '#e1c8c4',
    borderRadius: 10,
    paddingHorizontal: 13,
    backgroundColor: '#fffafa',
  },
  removeButtonText: { color: '#8e3b32', fontSize: 14, fontWeight: '700' },
  operationStatus: { flexDirection: 'row', alignItems: 'center', gap: 9 },
  operationText: { color: '#52605b', fontSize: 14, lineHeight: 20 },
  errorText: { color: '#8e3b32', fontSize: 14, lineHeight: 21 },
  interpretButton: {
    minHeight: 52,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: 14,
    backgroundColor: '#28785f',
  },
  interpretButtonDisabled: { backgroundColor: '#dce5e1' },
  interpretButtonText: { color: '#ffffff', fontSize: 16, fontWeight: '700' },
  interpretButtonTextDisabled: { color: '#7c8984' },
  buttonDisabled: { opacity: 0.52 },
  buttonPressed: { opacity: 0.78 },
});
