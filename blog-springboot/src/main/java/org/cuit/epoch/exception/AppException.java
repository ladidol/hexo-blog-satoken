package org.cuit.epoch.exception;

import lombok.AllArgsConstructor;
import lombok.Getter;
import org.cuit.epoch.enums.StatusCodeEnum;

import static org.cuit.epoch.enums.StatusCodeEnum.FAIL;

/**
 * @author: ladidol
 * @date: 2022/9/27 23:13
 * @description:
 */
@Getter
@AllArgsConstructor
public class AppException extends RuntimeException {
    /**
     * 错误码
     */
    private Integer code = FAIL.getCode();

    /**
     * 错误信息
     */
    private final String message;

    public AppException(String message) {
        this.message = message;
    }

    public AppException(StatusCodeEnum statusCodeEnum) {
        this.code = statusCodeEnum.getCode();
        this.message = statusCodeEnum.getDesc();
    }
}
