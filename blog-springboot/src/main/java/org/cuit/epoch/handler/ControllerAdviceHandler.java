package org.cuit.epoch.handler;

import cn.dev33.satoken.exception.NotLoginException;
import lombok.extern.log4j.Log4j2;
import org.cuit.epoch.exception.AppException;
import org.cuit.epoch.util.Result;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

import java.util.Objects;

import static org.cuit.epoch.enums.StatusCodeEnum.SYSTEM_ERROR;
import static org.cuit.epoch.enums.StatusCodeEnum.VALID_ERROR;

/**
 * @author: ladidol
 * @date: 2022/11/16 16:53
 * @description:
 */
@Log4j2
@RestControllerAdvice
public class ControllerAdviceHandler {

    /**
     * 处理服务异常
     *
     * @param e 异常
     * @return 接口异常信息
     */
    @ExceptionHandler(value = AppException.class)
    public Result<?> errorHandler(AppException e) {
        return Result.fail(e.getCode(), e.getMessage());
    }

    /**
     * 处理参数校验异常
     *
     * @param e 异常
     * @return 接口异常信息
     */
    @ExceptionHandler(MethodArgumentNotValidException.class)
    public Result<?> errorHandler(MethodArgumentNotValidException e) {
        return Result.fail(VALID_ERROR.getCode(), Objects.requireNonNull(e.getBindingResult().getFieldError()).getDefaultMessage());
    }

    /**
     * 处理SaToken异常
     *
     * @param e 异常
     * @return 接口异常信息
     */
    @ExceptionHandler(value = NotLoginException.class)
    public Result<?> errorHandler(NotLoginException e) {
        // 如果是未登录异常
        log.warn(e.getMessage());
        return Result.fail("登录过期 or 未登录，请尝试重新登录");
    }


    /**
     * 处理系统异常
     *
     * @param e 异常
     * @return 接口异常信息
     */
    @ExceptionHandler(value = Exception.class)
    public Result<?> errorHandler(Exception e) {
        e.printStackTrace();
        return Result.fail(SYSTEM_ERROR.getCode(), SYSTEM_ERROR.getDesc());
    }


}